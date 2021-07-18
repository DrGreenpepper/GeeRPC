package geerpc

import ( 
	"encoding/json"
	"errors"
	"fmt"
	"geerpc/codec"
	"io"
	"log"
	"net"
	"sync"
)
//调用信息
type Call struct {
	Seq				uint64		//序列号
	ServiceMethod	string		//类型
	Args			interface{}	//参数
	Reply			interface{}	//返回
	Error			error		
	Done			chan *Call	
}

//支持异步调用。
func (call *Call) done() {
	call.Done <- call
}

//发送器
type Client struct {
	cc			codec.Codec			//编解码器
	opt			*Option				//声明编码选项及服务类型
	sending		sync.Mutex			//头发送锁，复用
	header		codec.Header
	mu			sync.Mutex			//pending和seq的读写锁
	seq			uint64
	pending		map[uint64]*Call	//未处理完的请求
	closing		bool				//用户端关闭
	shutdown	bool				//服务器关闭||断开连接
}
//保证实现

var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

//关闭连接
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

//是否可用
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

//跟call有关的

//添加调用
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}
//移除调用，并返回
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Lock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}
//服务器错误时，对所用pending的call通知
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}


//监听回复，解析回复
func (client *Client) receive() {
	var err error
	// 在没有错误时一直读取
	for err == nil {
		var h codec.Header 
		
		//cc中包含了conn，所以会自动从已经连接的conn读取header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			//写入失败或被移除
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body" + err.Error())
			}
			call.done()
		}
	}
	client.terminateCalls(err)
}

//<-------------新建一个client和连接过程---------------->
//建立连接
func Dial(network, address string, opts ...*Option) (client *Client,err error) {
	opt, err := parseOption(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	return NewClient(conn, opt)
}

//解析options
func parseOption(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

// 新建Client
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client error:", err)
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error: ", err)
		_ = conn.Close()
		return nil, err
	}
	//f函数新建了一个codec,这个codec包含了连接
	return newClientCodec(f(conn), opt), nil
}

//设置其他参数，开始监听回复
func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		seq:		1,
		cc:			cc,
		opt:		opt,
		pending:	make(map[uint64]*Call),
	}
	go client.receive()
	return client
}
//<-------------结束------------>

//<----------------------发起一个新调用------------------------->
func (client *Client) Call(serviceMethod string, args, reply interface{}) error {
	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}

//异步调用方法，返回调用的invocation
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	}	else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod:	serviceMethod,
		Args:			args,
		Reply:			reply,
		Done:			done,
	}
	client.send(call)
	return call
}

//向远端发起调用
func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()

	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	if err := client.cc.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

//<-----------------结束------------------------->



