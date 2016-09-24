package nntp

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"strconv"

	"github.com/pkg/errors"
	"github.com/thebigjc/nzb/nzbfile"
	"github.com/thebigjc/nzb/yenc"
)

var CRLF []byte = []byte{'\r', '\n'}

type NNTPConfInfo struct {
	port   int
	host   string
	user   string
	pass   string
	useTls bool
	outDir string
}

func BuildNNTPWorkers(workQueue chan *nzbfile.SegmentRequest, conn, port int, host, user, pass string, useTls bool, outDir string) {
	connInfo := NNTPConfInfo{
		port,
		host,
		user,
		pass,
		useTls,
		outDir,
	}

	var workerQueue chan chan *nzbfile.SegmentRequest
	workerQueue = make(chan chan *nzbfile.SegmentRequest, conn)

	for i := 0; i < conn; i++ {
		log.Println("Opening connection:", i+1)
		worker := NewNNTPWorker(i+1, workerQueue, &connInfo)
		go worker.Start()
	}

	go func() {
		for {
			select {
			case work := <-workQueue:
				//log.Println("Received work request")
				go func() {
					worker := <-workerQueue

					//log.Println("Dispatching work request")
					worker <- work
				}()
			}
		}
	}()
}

type NNTPWorker struct {
	ID          int
	Work        chan *nzbfile.SegmentRequest
	WorkerQueue chan chan *nzbfile.SegmentRequest
	QuitChan    chan bool
	Config      *NNTPConfInfo
	rw          *bufio.ReadWriter
}

func NewNNTPWorker(id int, workerQueue chan chan *nzbfile.SegmentRequest, connInfo *NNTPConfInfo) NNTPWorker {
	// Create, and return the worker.
	worker := NNTPWorker{
		ID:          id,
		Work:        make(chan *nzbfile.SegmentRequest),
		WorkerQueue: workerQueue,
		QuitChan:    make(chan bool),
		Config:      connInfo,
	}

	return worker
}

func (w *NNTPWorker) ReadRetcode() (retcode int, err error) {
	line, _, err := w.rw.ReadLine()

	if err != nil {
		return -1, err
	}

	retcode, err = strconv.Atoi(string(line[:3]))

	return retcode, err
}

func (w *NNTPWorker) SendLine(line string, expectedRet int) (retcode int, err error) {
	_, err = w.rw.Write([]byte(line))
	if err != nil {
		return -1, err
	}

	_, err = w.rw.Write(CRLF)
	if err != nil {
		return -1, err
	}

	err = w.rw.Flush()
	if err != nil {
		return -1, err
	}

	retcode, err = w.ReadRetcode()
	if retcode != expectedRet {
		return retcode, errors.Errorf("Unexpected retcode %d != %d", retcode, expectedRet)
	}

	return retcode, nil
}

type CounterReader struct {
	Count int
	r     io.Reader
}

func (c *CounterReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	c.Count += n
	return n, err
}

func (w *NNTPWorker) Start() {
	if w.Config.useTls {
		port := w.Config.port
		if port == 0 {
			port = 563
		}

		var err error

		conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", w.Config.host, port), nil)
		if err != nil {
			log.Panicf("Worker %d couldn't connect %v\n", w.ID, err)
			return
		}

		w.rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		retcode, err := w.ReadRetcode()

		if err != nil {
			log.Panicln("Failed to get hello line", err)
		}

		if retcode != 200 {
			log.Panicln("Unexpected retcode:", retcode)
		}

		log.Println("Connected", w.ID)

		userLoginStr := fmt.Sprintf("AUTHINFO USER %s", w.Config.user)
		retcode, err = w.SendLine(userLoginStr, 381)

		if err != nil {
			log.Panicln("User failed", err)
		}

		userPassStr := fmt.Sprintf("AUTHINFO PASS %s", w.Config.pass)
		retcode, err = w.SendLine(userPassStr, 281)

		if err != nil {
			log.Panicln("Pass failed", err)
		}

		log.Println("Logged in", w.ID)
	}

	go func() {
		for {
			// Add ourselves into the worker queue.
			w.WorkerQueue <- w.Work

			select {
			case work := <-w.Work:
				// Receive a work request.
				//log.Printf("worker%d: Received work request, fetching Message ID %s\n", w.ID, work.MessageID)
				articleLine := fmt.Sprintf("BODY <%s>", work.MessageID)
				retcode, err := w.SendLine(articleLine, 222)
				if err != nil {
					log.Println("Failed to fetch article", work.MessageID, retcode, err)
					work.Observe(0)
					break
				}

				cr := &CounterReader{r: w.rw.Reader}
				y, err := yenc.ReadYenc(cr)

				if err != nil {
					log.Fatalln("Failed to read body", err)
				}

				go func(y *yenc.Yenc, s *nzbfile.SegmentRequest, outDir string, count int) {
					w, err := s.BuildWriter(outDir, y.Name)
					if err != nil {
						log.Fatalln("Failed to create writer", err)
					}
					err = y.SaveBody(w)
					if err != nil {
						log.Fatalln("Failed to save message body.", err)
					}
					s.Observe(count)
				}(y, work, w.Config.outDir, cr.Count)

			case <-w.QuitChan:
				// We have been asked to stop.
				log.Printf("worker%d stopping\n", w.ID)
				return
			}
		}
	}()
}

// Stop tells the worker to stop listening for work requests.
//
// Note that the worker will only stop *after* it has finished its work.
func (w *NNTPWorker) Stop() {
	go func() {
		w.QuitChan <- true
	}()
}
