package grpc_conn

type Pool struct {
	// indexed by 'name'
	conns map[string]*Conn
}

// new gRPC Pool of *Conn. Indexed only by 'name' (so that must be unique)
func NewPool(xs ...*Conn) (*Pool, error) {
	p := &Pool{conns: map[string]*Conn{}}

	for _, x := range xs {
		p.conns[x.GetName()] = x
	}
	return p, nil
}

func (p *Pool) Get(name string) (*Conn, bool) {
	c, found := p.conns[name]
	return c, found
}
