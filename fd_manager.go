package nutsdb

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const (
	// MaxFdNumInLinux means max opening fd numbers of Linux is 1024
	MaxFdNumInLinux uint = 1024
	// MaxFdNumInWindows means max opening fd numbers of Windows is 512
	MaxFdNumInWindows uint = 512
	// DefaultMaxFdNums means max opening fd numbers beside Windows and Linux
	DefaultMaxFdNums uint = 1024
)

//getMaxFdNumsInSystem
func getMaxFdNumsInSystem() uint {
	switch runtime.GOOS {
	case "linux":
		return MaxFdNumInLinux
	case "windows":
		return MaxFdNumInWindows
	default:
		return DefaultMaxFdNums
	}
}

func newFdm(maxFdNums uint, cleanThreshold float64) (fdm *fdManager) {
	fdm = &fdManager{
		cache:          map[string]*FdInfo{},
		fdList:         initList(),
		size:           0,
		maxFdNums:      getMaxFdNumsInSystem(),
		cleanThreshold: 0.5,
	}
	if maxFdNums > 0 {
		fdm.maxFdNums = maxFdNums
	}

	if cleanThreshold < 0.5 && cleanThreshold > 0.0 {
		fdm.cleanThreshold = cleanThreshold
	}
	return fdm
}

func (fdm *fdManager) setOptions(maxFdNums uint, cleanThreshold float64) {
	if maxFdNums > 0 {
		fdm.maxFdNums = maxFdNums
	}

	if cleanThreshold < 0.5 && cleanThreshold > 0.0 {
		fdm.cleanThreshold = cleanThreshold
	}
}

type fdManager struct {
	sync.Mutex
	cache          map[string]*FdInfo
	fdList         *doubleLinkedList
	size           uint
	cleanThreshold float64
	maxFdNums      uint
}

type FdInfo struct {
	fd    *os.File
	path  string
	using uint
	next  *FdInfo
	prev  *FdInfo
}

func (fdm *fdManager) getFd(path string) (fd *os.File, err error) {
	fdm.Lock()
	defer fdm.Unlock()
	cleanPath := filepath.Clean(path)
	if fdInfo := fdm.cache[cleanPath]; fdInfo == nil {
		fd, err = os.OpenFile(cleanPath, os.O_CREATE|os.O_RDWR, 0644)
		if err == nil {
			fdInfo := &FdInfo{
				fd:    fd,
				using: 1,
				path:  cleanPath,
			}
			fdm.fdList.addNode(fdInfo)
			fdm.size++
			fdm.cache[cleanPath] = fdInfo
			if fdm.size >= fdm.maxFdNums {
				cleanNums := int(math.Floor(fdm.cleanThreshold * float64(fdm.size)))
				node := fdm.fdList.tail.prev
				for node != nil && node != fdm.fdList.head && cleanNums > 0 {
					nextItem := node.prev
					if node.using == 0 {
						fdm.fdList.remoteNode(node)
						err := node.fd.Close()
						if err != nil {
							return nil, err
						}
						fdm.size--
						delete(fdm.cache, node.path)
						cleanNums--
					}
					node = nextItem
				}
			}
		}
		return fd, err
	} else {
		fdInfo.using++
		fdm.fdList.moveNodeToFront(fdInfo)
		return fdInfo.fd, nil
	}
}

func (fdm *fdManager) reduceUsing(path string) {
	fdm.Lock()
	defer fdm.Unlock()
	cleanPath := filepath.Clean(path)
	node := fdm.cache[cleanPath]
	if node == nil {
		panic("unexpected the node is not in cache")
	}
	node.using--
}

func (fdm *fdManager) close() error {
	fdm.Lock()
	defer fdm.Unlock()
	node := fdm.fdList.tail.prev
	for node != fdm.fdList.head {
		err := node.fd.Close()
		if err != nil {
			return err
		}
		delete(fdm.cache, node.path)
		fdm.size--
		node = node.prev
	}
	fdm.fdList.head.next = fdm.fdList.tail
	fdm.fdList.tail.prev = fdm.fdList.head
	return nil
}

type doubleLinkedList struct {
	head *FdInfo
	tail *FdInfo
	size int
}

func initList() *doubleLinkedList {
	list := &doubleLinkedList{
		head: &FdInfo{},
		tail: &FdInfo{},
		size: 0,
	}
	list.head.next = list.tail
	list.tail.prev = list.head
	return list
}

func (list *doubleLinkedList) addNode(node *FdInfo) {
	list.head.next.prev = node
	node.next = list.head.next
	list.head.next = node
	node.prev = list.head
	list.size++
}

func (list *doubleLinkedList) remoteNode(node *FdInfo) {
	node.prev.next = node.next
	node.next.prev = node.prev
	node.prev = nil
	node.next = nil
}

func (list *doubleLinkedList) moveNodeToFront(node *FdInfo) {
	list.remoteNode(node)
	list.addNode(node)
}
