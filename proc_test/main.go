package main

import (
	"log"
	"os"
	"proc_test/logging"
	"proc_test/port"
	"syscall"
)

func monitorPorts(portList []*port.Port) {
	epfd, err := syscall.EpollCreate1(0)
	if err != nil {
		log.Fatal(err)
	}
	defer syscall.Close(epfd)

	portMap := make(map[int32]*port.Port)

	for _, port := range portList {
		filename := port.ProcFile()
		file, err := os.Open(filename)
		if err != nil {
			log.Fatal(err)
		}

		portMap[int32(file.Fd())] = port

		var event syscall.EpollEvent
		event.Events = syscall.EPOLLIN
		event.Fd = int32(file.Fd())

		err = syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, int(file.Fd()), &event)
		if err != nil {
			log.Fatal(err)
		}

	}

	for {
		events := make([]syscall.EpollEvent, 1)
		_, err = syscall.EpollWait(epfd, events, -1)
		if err != nil {
			log.Fatal(err)
		}

		for _, event := range events {
			port := portMap[event.Fd]
			go handleActivePort(port)
		}
	}

}

func handleActivePort(p *port.Port) {
	filename := p.ProcFile()
	content, err := os.ReadFile(filename)
	if err != nil {
		logging.Error("read file %s failed: %v", filename, err)
	}
	returnMsg := ""
	switch content[0] {
	case '1':
		err := p.AddBackends()
		if err != nil {
			logging.Error("add backends failed: %v", err)
		}
		if p.State == port.PortStarted {
			returnMsg = "2"
		}

	case '3':
		err := p.RemoveBackends()
		if err != nil {
			logging.Error("remove backends failed: %v", err)
		}
		if p.State == port.PortStopped {
			returnMsg = "0"
		}
	}

	if returnMsg == "" {
		return
	}

	if err := os.WriteFile(filename, []byte(returnMsg), 0644); err != nil {
		logging.Error("write file %s failed: %v", filename, err)
	}
}

func addPort(portList []*port.Port, num int, protocal string) ([]*port.Port, error) {
	port := port.NewPort(num, protocal)
	if err := port.Setup(); err != nil {
		return nil, err
	}
	portList = append(portList, port)
	return portList, nil
}

func main() {

	if len(os.Args) >= 2 && os.Args[1] == "cleanup" {
		for i := 10600; i < 10700; i++ {
			port := port.NewPort(i, "tcp")
			if err := port.Shutdown(); err != nil {
				log.Fatal(err)
			}
		}
		return
	}

	portList := make([]*port.Port, 0)
	var err error
	for i := 10600; i < 10700; i++ {
		portList, err = addPort(portList, i, "tcp")
		if err != nil {
			log.Fatal(err)
		}
	}

	monitorPorts(portList)
}
