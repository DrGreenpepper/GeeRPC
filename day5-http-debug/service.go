package geerpc

import(
	"reflect"
	"sync/atomic"
	"log"
	"go/ast"
)

//方法类型
type methodType struct {
	method		reflect.Method		//方法
	ArgType		reflect.Type		//传入参数
	ReplyType	reflect.Type		//回传参数
	numCalls	uint64				//调用次数
}
//查看被调用次数
func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}
//新建参数类型实例
func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	if m.ArgType.Kind() == reflect.Ptr {
//如果是指针的话，那么可以访问指针的elem()，并new出
		argv = reflect.New(m.ArgType.Elem())
	} else {
//如果不是指针的话，要先new出类型，new后访问返回的指针的elem()
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}
//同上
func (m *methodType) newReplyv() reflect.Value {
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}


//服务类型
type service struct {
	name	string					//映射的名称
	typ		reflect.Type			//结构体类型
	rcvr	reflect.Value			//实例
	method  map[string]*methodType	//符合条件的方法
}
//构造函数
//rcvr是接收器，就是一个带方法的type
func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	//使用indirect是为了防止rcvr是指针类型，如果是指针类型，就可以转换为他具体值的实例
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ  = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}
//注册方法
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		//入参
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		//出参是否为error
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue;
		}
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		//登记在method的map种
		s.method[method.Name] = &methodType {
			method 		: method,
			ArgType		: argType,
			ReplyType	: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	//原子操作，并发安全
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	//通过反射调用方法
	//方一个reflect.Value切片，分别是s.rcvr结构体本身,argv参数,reoplyv返回参数
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
