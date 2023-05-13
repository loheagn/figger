package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"proc_test/logging"
	"proc_test/port"

	"golang.org/x/sys/unix"
)

type PortAction struct {
	num      int
	protocal string
	add      bool // true: add port, false: remove port
}

func monitor(portActionChan <-chan *PortAction, stopChan <-chan struct{}) {
	epfd, err := unix.EpollCreate1(0)
	if err != nil {
		log.Fatal(err)
	}
	defer unix.Close(epfd)

	procFile2PortMap := make(map[int]*port.Port)
	portName2ProFileMap := make(map[string]int)

	addPort := func(action *PortAction) error {
		p := port.NewPort(action.num, action.protocal)
		if err := p.Setup(); err != nil {
			return err
		}

		filename := p.ProcFile()
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := file.Write([]byte("0")); err != nil {
			return err
		}

		err = unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, int(file.Fd()), &unix.EpollEvent{Events: unix.POLLIN | unix.POLLHUP, Fd: int32(file.Fd())})
		if err != nil {
			return err
		}

		procFile2PortMap[int(file.Fd())] = p
		portName2ProFileMap[p.String()] = int(file.Fd())

		return nil
	}

	removePort := func(action *PortAction) error {
		portName := fmt.Sprintf("%s-%d", action.protocal, action.num)
		fd := portName2ProFileMap[portName]
		p := procFile2PortMap[fd]

		err := unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, int(fd), nil)
		if err != nil {
			return err
		}

		if err := p.Shutdown(); err != nil {
			return err
		}

		delete(portName2ProFileMap, portName)
		delete(procFile2PortMap, fd)

		return nil
	}

	stopAll := func() {
		for _, p := range procFile2PortMap {
			_ = p.Shutdown()
		}
	}

	epollEventsChan := make(chan []unix.EpollEvent)

	go func() {
		for {
			events := make([]unix.EpollEvent, 1)
			_, err = unix.EpollWait(epfd, events, -1)
			if err != nil {
				log.Fatal(err)
			}
			epollEventsChan <- events
		}
	}()

	for {
		select {
		case events := <-epollEventsChan:
			for _, event := range events {
				port := procFile2PortMap[int(event.Fd)]
				go handleActivePort(port)
			}

		case portAction := <-portActionChan:
			switch portAction.add {
			case true:
				err := addPort(portAction)
				if err != nil {
					logging.Error("add port %d failed: %v", portAction.num, err)
				}
			case false:
				err := removePort(portAction)
				if err != nil {
					logging.Error("remove port %d failed: %v", portAction.num, err)
				}
			}

		case <-stopChan:
			stopAll()
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

func setupHTTPServer(portActionChan chan<- *PortAction, stopChan chan<- struct{}) {
	mux := http.NewServeMux()
	mux.Handle("/stop", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stopChan <- struct{}{}

		_, err := w.Write([]byte("stop all ports"))
		if err != nil {
			logging.Error("write response failed: %v", err)
		}
	}))
	mux.Handle("/add", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		num, _ := strconv.Atoi(r.URL.Query().Get("num"))
		protocol := r.URL.Query().Get("protocol")
		portActionChan <- &PortAction{num: num, protocal: strings.ToLower(protocol), add: true}

		_, err := w.Write([]byte(fmt.Sprintf("add port %d", num)))
		if err != nil {
			logging.Error("write response failed: %v", err)
		}
	}))
	mux.Handle("/remove", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		num, _ := strconv.Atoi(r.URL.Query().Get("num"))
		protocol := r.URL.Query().Get("protocol")
		portActionChan <- &PortAction{num: num, protocal: strings.ToLower(protocol), add: false}

		_, err := w.Write([]byte(fmt.Sprintf("remove port %d", num)))
		if err != nil {
			logging.Error("write response failed: %v", err)
		}
	}))

	err := http.ListenAndServe(":8899", mux)
	if err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}

func main() {
	portActionChan := make(chan *PortAction)
	stopChan := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		monitor(portActionChan, stopChan)
		wg.Done()
	}()
	go func() {
		setupHTTPServer(portActionChan, stopChan)
		wg.Done()
	}()
	wg.Wait()
}
