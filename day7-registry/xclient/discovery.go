package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

type SelectMode int

//随机选择，轮询模式
const (
	RandomSelect	SelectMode = iota
	RoundRobinSelect
)
//服务发现的基本接口

type Discovery interface {
//刷新
	Refresh() error
//更新服务列表
	Update(servers []string) error
//根据负载均衡策略选择服务实例
	Get(mode SelectMode) (string, error)
//返回所有服务实例
	GetAll() ([]string, error)
}


//手工维护的服务发现结构体
type MultiServersDiscovery struct {
	//随机数，用于生成轮询
	r 		*rand.Rand 
	//读写锁
	mu		sync.RWMutex
	//服务器们
	servers []string
	//当前被选的轮询序号
	index	int
}
//构造函数直接通过已有的servers建立
func NewMultiServerDiscovery(servers []string) *MultiServersDiscovery{
	d 	:= &MultiServersDiscovery {
		servers:	servers,
		r:			rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	d.index = d.r.Intn(math.MaxInt32-1)
	return d
}


var _ Discovery = (*MultiServersDiscovery)(nil)
//目前是手动维护，所以刷新功能没啥用
func (d *MultiServersDiscovery) Refresh() error {
	return nil
}//更新服务器servers
func (d *MultiServersDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	return nil
}
//根据模式获取一个服务器
func (d *MultiServersDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.servers)
	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}
	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		s := d.servers[d.index%n]
		d.index = (d.index + 1) % n 
		return s, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}
//返回服务器列表
func (d *MultiServersDiscovery) GetAll() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	servers := make([]string, len(d.servers), len(d.servers))
	copy(servers, d.servers)
	return servers, nil
}