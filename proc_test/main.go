package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

// read proc file
func readProcFile(filename string) {

	content, _ := os.ReadFile(filename)

	log.Printf("File content: %s\n", content)
}

type fdset syscall.FdSet

func (s *fdset) Sys() *syscall.FdSet {
	return (*syscall.FdSet)(s)
}

func (s *fdset) Set(fd uintptr) {
	bits := 8 * unsafe.Sizeof(s.Bits[0])
	if fd >= bits*uintptr(len(s.Bits)) {
		panic("fdset: fd out of range")
	}
	n := fd / bits
	m := fd % bits
	s.Bits[n] |= 1 << m
}

func (s *fdset) IsSet(fd uintptr) bool {
	bits := 8 * unsafe.Sizeof(s.Bits[0])
	if fd >= bits*uintptr(len(s.Bits)) {
		panic("fdset: fd out of range")
	}
	n := fd / bits
	m := fd % bits
	return s.Bits[n]&(1<<m) != 0
}

func readAndTrigger(filename string) {
	readProcFile(filename)

	err := exec.Command("bash", "-c", "systemctl start nginx").Run()
	if err != nil {
		panic(err)
	}
	err = exec.Command("bash", "-c", "ipvsadm -a -t 192.168.174.134:6699 -r 192.168.174.134:80 -m -w 1").Run()
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(filename, []byte("hello world"), 0644)
	if err != nil {
		panic(err)
	}
}

func cleanup() {
	err := exec.Command("bash", "-c", "systemctl stop nginx").Run()
	if err != nil {
		panic(err)
	}
	err = exec.Command("bash", "-c", "ipvsadm -D -t 192.168.174.134:6699").Run()
	if err != nil {
		panic(err)
	}
	err = exec.Command("bash", "-c", "ipvsadm -A -t 192.168.174.134:6699 -s rr").Run()
	if err != nil {
		panic(err)
	}
}

// syscall.Select file
func selectFile(filename string) {
	var fd fdset
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	fd.Set(file.Fd())
	n, err := syscall.Select(int(file.Fd())+1, fd.Sys(), nil, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("ready %d\n", n)

	log.Printf("isset %v\n", fd.IsSet(file.Fd()))

	readAndTrigger(filename)
}

func epoll_test(filename string) {
	epfd, err := syscall.EpollCreate1(0)
	if err != nil {
		log.Fatal(err)
	}
	defer syscall.Close(epfd)

	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}

	var event syscall.EpollEvent
	event.Events = syscall.EPOLLIN
	event.Fd = int32(file.Fd())

	err = syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, int(file.Fd()), &event)
	if err != nil {
		log.Fatal(err)
	}

	events := make([]syscall.EpollEvent, 1)
	n, err := syscall.EpollWait(epfd, events, -1)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("ready %d\n", n)

	for _, event := range events {
		if event.Fd == int32(file.Fd()) {
			readAndTrigger(filename)
		}
	}

}

func main() {
	// selectFile(os.Args[1])
	epoll_test(os.Args[1])

	time.Sleep(10 * time.Second)
	cleanup()
}
