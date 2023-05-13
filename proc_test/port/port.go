package port

import (
	"fmt"
	"os"
	"proc_test/cmd"
	"strings"
	"sync"
)

const HOST_IP = "FIGGER_HOST_IP"

type Protocal string

const (
	TCP Protocal = "tcp"
	UDP Protocal = "udp"
)

type PortState int

const (
	PortStarting PortState = iota
	PortStarted
	PortStopping
	PortStopped
)

type Port struct {
	Num      int
	Protocal Protocal

	State PortState

	mutex sync.Mutex
}

func NewPort(num int, protocal string) *Port {
	return &Port{Num: num, Protocal: Protocal(strings.ToLower(protocal)), State: PortStopped, mutex: sync.Mutex{}}
}

func (p *Port) String() string {
	return fmt.Sprintf("%s-%d", p.Protocal, p.Num)
}

func (p *Port) formatIPVSArg() string {
	arg := "-t"
	if p.Protocal == UDP {
		arg = "-u"
	}
	return fmt.Sprintf("%s %s:%d", arg, HostIP(), p.Num)
}

func (p *Port) execIPVSCmd(setup bool) error {
	arg := "-A"
	tailArg := "-s rr"
	if !setup {
		arg = "-D"
		tailArg = ""
	}
	command := fmt.Sprintf("ipvsadm %s %s %s", arg, p.formatIPVSArg(), tailArg)
	return cmd.Exec(command)
}

func (p *Port) ProcFile() string {
	return fmt.Sprintf("/proc/figger/%s-%d", p.Protocal, p.Num)
}

func (p *Port) Setup() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.execIPVSCmd(true)
}

func (p *Port) AddBackends() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.State == PortStarting || p.State == PortStarted {
		return nil
	}

	oldState := p.State
	p.State = PortStarting

	var err error
	defer func() {
		if err != nil {
			p.State = oldState
		} else {
			p.State = PortStarted
		}
	}()

	err = cmd.Exec("systemctl start nginx")
	if err != nil {
		return err
	}

	command := fmt.Sprintf("ipvsadm -a %s -r %s:80 -m -w 1", p.formatIPVSArg(), HostIP())
	err = cmd.Exec(command)
	if err != nil {
		return err
	}

	return nil
}

func (p *Port) RemoveBackends() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.State == PortStopping || p.State == PortStopped {
		return nil
	}

	oldState := p.State
	p.State = PortStopping

	var err error
	defer func() {
		if err != nil {
			p.State = oldState
		} else {
			p.State = PortStopped
		}
	}()

	command := fmt.Sprintf("ipvsadm -d %s -r %s:80", p.formatIPVSArg(), HostIP())
	err = cmd.Exec(command)
	if err != nil {
		return err
	}

	err = cmd.Exec("systemctl stop nginx")
	if err != nil {
		return err
	}
	return nil
}

func (p *Port) Shutdown() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return p.execIPVSCmd(false)
}

func HostIP() string {
	return os.Getenv(HOST_IP)
}
