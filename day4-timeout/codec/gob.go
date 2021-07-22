package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)
//gob是golang包自带的一个数据结构序列化的编码/解码工具。
//
type GobCodec struct {
	conn	io.ReadWriteCloser	//远程连接
	buf		*bufio.Writer		//缓冲区
	dec		*gob.Decoder		//远端信息拿来解码
	enc		*gob.Encoder		//本地信息拿去编码
}


//实例化一个Codec接口的GobCodec指针类型，保证GobCodec实现了Codec接口。
//由于是_实例，所以不用担心声明未使用的问题，在编译期间协助检查。
var _ Codec = (*GobCodec)(nil)

//工厂  构造函数
func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec {
		conn:	conn,
		buf:	buf,
		dec:	gob.NewDecoder(conn),
		enc:	gob.NewEncoder(buf),
	}
}

//gob.Decode()
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

//gob.Decide
func (c *GobCodec) ReadBody (body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err := c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}

	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body", err)
		return err
	}
	return nil
}

// 关闭远程链接
func (c *GobCodec) Close() error {
	return c.conn.Close()
}