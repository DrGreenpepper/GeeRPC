package geerpc

import (
	"geerpc/codec"
	"io"
	"net"
	"log"
	"sync"
	"fmt"
	"reflect"
	"encoding/json"
)

const MagicNumber = 0x3b3f5c	//标记为geerpc的请求格式
//参数格式，固定json编码
type Option struct {
	MagicNumber	int			//表示请求类型
	CodecType	codec.Type	//可选codec接口
}
//默认格式
var DefaultOption = &Option {
	MagicNumber	:	MagicNumber,
	CodecType	:	codec.GobType,
}

//Server类型
type Server struct{}

func NewServer() *Server {
	return &Server{}
}

var DefalultServer = NewServer()

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
	DefalultServer.Accept(lis)
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
	server.serveCodec(f(conn))
}

//解码器
var invalidRequest = struct{}{}

func (server *Server) serveCodec (cc codec.Codec) {
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
		go server.handleRequest(cc, req, sending, wg)
	}
	//等待所有协程执行完毕
	wg.Wait()
	_ = cc.Close()
}

// /请求结构体
type request struct {
	h				*codec.Header
	argv, replyv	reflect.Value
}

//读取请求，传入解码器，返回解析的请求
func (server *Server) readRequest(cc codec.Codec ) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	//TODO:还未确定请求的参数
	//只需要支持string
	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv err:", err)
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

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	//TODO: 需要调用已注册的method，得到回复
	//day1 1, 只输出参数，然后送回hello！
	defer wg.Done()
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("geerpc resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}