package codec

import "io"

//抽象的请求
type Header struct {
	ServiceMethod	string	//	format 服务.方法
	Seq				uint64	//	序列号
	Error			string
}

//	编码接口
type Codec interface {
	io.Closer
	ReadHeader(*Header) error			//读取header方法
	ReadBody(interface{}) error			//读取body方法
	
	Write(*Header, interface{}) error	//写入header和body方法
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const(
	GobType 	Type	= "application/gob"
	JsonType	Type	= "application/json"	//不去实现
)

//将编码解码方式映射，工厂模式，返回一个构造函数
var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec	//实现GobCodec接口的函数
}

