package geerpc

import (
	"geerpc/codec"
	"io"
	"net"
	"log"
	"sync"
	"reflect"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"fmt"
)

const MagicNumber = 0x3b3f5c	//标记为geerpc的请求格式
//参数格式，固定json编码
type Option struct {
	MagicNumber		int			//表示请求类型
	CodecType		codec.Type	//可选codec接口

	ConnectTimeout	time.Duration
	HandleTimeout	time.Duration
}
//默认格式
var DefaultOption = &Option {
	MagicNumber		:	MagicNumber,
	CodecType		:	codec.GobType,
	//默认十秒超时
	ConnectTimeout	:	time.Second * 10,
}

//day3---------
//Server类型  
//线程安全map
type Server struct {
	serviceMap sync.Map
}
//注册方法，传入一个reciver
func (server *Server) Register (rcvr interface{}) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defined: " + s.name)
	}
	return nil
}

func Register (rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed:" + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	//查找到service
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}
	//通过service查找method
	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}
//              | 
//day3----------

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

//server方法,与监听。
func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go server.ServeConn(conn)
	}
}

//启动服务的函数，只需传入一个listener就行.
func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

//连接器，验证这个连接是否为合法连接
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()

	//解析Option,验证是否为合理请求
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}

	//获取解码器
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f==nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}

	//合理请求则继续解码，f() 是上面解码器函数。
	server.serveCodec(f(conn), &opt)
}

//解码器
var invalidRequest = struct{}{}

func (server *Server) serveCodec (cc codec.Codec, opt *Option) {
	//互斥发送锁和等待队列信号量
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		//读取请求,没有请求时，结束。
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		//并发处理请求
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	//等待所有协程执行完毕
	wg.Wait()
	_ = cc.Close()
}

//-------------------------------------------------------------------------------
//-------------------------------------------------------------------------------
// /请求结构体
//day3------------------------------
//              
type request struct {
	h				*codec.Header
	argv, replyv	reflect.Value
	mtype			*methodType
	svc				*service
}

//读取请求，传入解码器，返回解析的请求
func (server *Server) readRequest(cc codec.Codec ) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()
	//创建入参实例
	//保证argvi是指针，readbody需要指针作为参数，将解析的内容返回。
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return req, err
	}
	return req, nil
}

//读取请求Header，传入解码器，返回一个已解析的Header
func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil,err
	}
	return &h, nil
}



func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called	 := make(chan struct{})
	sent	 := make(chan struct{})
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()
	
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}