package nntp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"strconv"

	"github.com/pkg/errors"
	"github.com/thebigjc/nzb/nzbfile"
	"github.com/thebigjc/nzb/yenc"
)

var crlf = []byte{'\r', '\n'}
var dotdot = []byte{'.', '.'}
var dot = []byte{'.'}

type ConfInfo struct {
	port       int
	host       string
	user       string
	pass       string
	useTLS     bool
	outDir     string
	numServers int
}

func BuildWorkers(workQueue chan *nzbfile.SegmentRequest, numServers, conn, port int, host, user, pass string, useTLS bool, outDir string) {
	connInfo := ConfInfo{
		port,
		host,
		user,
		pass,
		useTLS,
		outDir,
		numServers,
	}

	for i := 0; i < conn; i++ {
		worker := NewNNTPWorker(i+1, workQueue, &connInfo)
		go worker.Start()
	}
}

type Worker struct {
	ID        int
	WorkQueue chan *nzbfile.SegmentRequest
	QuitChan  chan bool
	Config    *ConfInfo
	Name      string
	rw        *bufio.ReadWriter
}

func NewNNTPWorker(id int, workQueue chan *nzbfile.SegmentRequest, connInfo *ConfInfo) Worker {
	// Create, and return the worker.
	worker := Worker{
		ID:        id,
		WorkQueue: workQueue,
		QuitChan:  make(chan bool),
		Config:    connInfo,
		Name:      fmt.Sprintf("%s:%d", connInfo.host, connInfo.port),
	}

	return worker
}

func (w *Worker) ReadRetcode() (retcode int, err error) {
	line, _, err := w.rw.ReadLine()

	if err != nil {
		return -1, err
	}

	retcode, err = strconv.Atoi(string(line[:3]))

	return retcode, err
}

func (w *Worker) SendLine(line string, expectedRet int) (retcode int, err error) {
	_, err = w.rw.Write([]byte(line))
	if err != nil {
		return -1, err
	}

	_, err = w.rw.Write(crlf)
	if err != nil {
		return -1, err
	}

	err = w.rw.Flush()
	if err != nil {
		return -1, err
	}

	retcode, err = w.ReadRetcode()
	if err != nil {
		return retcode, err
	}

	if retcode != expectedRet {
		return retcode, errors.Errorf("Unexpected retcode %d != %d", retcode, expectedRet)
	}

	return retcode, nil
}

func ReadBody(r *bufio.Reader) (body []byte, err error) {
	var b bytes.Buffer

	for {
		line, _, err := r.ReadLine()

		if err == io.EOF {
			return b.Bytes(), err
		}

		if err != nil {
			return nil, err
		}

		if bytes.HasPrefix(line, dotdot) {
			line = line[1:]
		}

		if bytes.Compare(line, dot) == 0 {
			return b.Bytes(), nil
		}

		b.Write(line)
		b.Write(crlf)
	}
}

func (w *Worker) Start() {
	if w.Config.useTLS {
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
	}

	go func() {
		for {
			select {
			case work := <-w.WorkQueue:
				if work.RequeueIfFailed(w.Name) {
					break
				}

				// Receive a work request.
				articleLine := fmt.Sprintf("BODY <%s>", work.MessageID)
				retcode, err := w.SendLine(articleLine, 222)

				if retcode == 430 {
					log.Println("Requeuing article", work.MessageID)
					work.FailServer(w.Name, w.Config.numServers)
					break
				}

				if err != nil {
					log.Println("Failed to fetch article", work.MessageID, retcode, err)
					work.FailServer(w.Name, w.Config.numServers)
					break
				}

				body, err := ReadBody(w.rw.Reader)

				y, err := yenc.ReadYenc(bytes.NewBuffer(body))

				if err != nil {
					log.Fatalln("Failed to read body", err)
				}

				go func(y *yenc.Yenc, s *nzbfile.SegmentRequest, outDir string, server string, numServers int) {
					w, err := s.BuildWriter(outDir, y.Name)
					defer w.Close()

					if err != nil {
						log.Println("Failed to create writer", err)
						s.FailServer(server, numServers)
						return
					}
					err = y.SaveBody(w)
					if err != nil {
						log.Println("Failed to save message body.", err)
						s.FailServer(server, numServers)
						return
					}
					s.Done(false)
				}(y, work, w.Config.outDir, w.Name, w.Config.numServers)

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
func (w *Worker) Stop() {
	go func() {
		w.QuitChan <- true
	}()
}
