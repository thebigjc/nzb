package nzbfile

import (
	"encoding/xml"
	"io"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/cheggaaa/pb"
)

type Nzb struct {
	File     []File `xml:"file"`
	jobName  string
	fileRe   *regexp.Regexp
	progress *pb.ProgressBar
}

type File struct {
	Poster  string    `xml:"poster,attr"`
	Date    string    `xml:"date,attr"`
	Subject string    `xml:"subject,attr"`
	Group   []string  `xml:"groups>group"`
	Segment []Segment `xml:"segments>segment"`
}

type Segment struct {
	Bytes     int64  `xml:"bytes,attr"`
	Number    int    `xml:"number,attr"`
	MessageID string `xml:",chardata"`
}

type SegmentRequest struct {
	MessageID     string
	Bytes         int64
	filename      string
	workGroup     *sync.WaitGroup
	nzb           *Nzb
	excludedHosts map[string]bool
	workQueue     chan *SegmentRequest
	failed        bool
	done          bool
}

type WriterAtCloser interface {
	io.WriterAt
	io.Closer
}

func (s *SegmentRequest) BuildWriter(outDir string, filename string) (WriterAtCloser, error) {
	dirName := path.Join(outDir, s.nzb.jobName)

	err := os.MkdirAll(dirName, 0700)
	if err != nil {
		return nil, err
	}

	outName := path.Join(outDir, s.nzb.jobName, filename)

	f, err := os.OpenFile(outName, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}

	return f, nil
}

func (s *SegmentRequest) Done() {
	if !s.done {
		s.done = true
		s.nzb.Complete(s.Bytes)
		s.workGroup.Done()
	}
}

func (s *SegmentRequest) FailServer(server string, numServers int) {
	log.Printf("Failing server %s for message %s\n", server, s.MessageID)
	if s.excludedHosts == nil {
		s.excludedHosts = make(map[string]bool)
	}
	s.excludedHosts[server] = true

	if len(s.excludedHosts) >= numServers {
		s.failed = true
		log.Println("Failing Article", s.MessageID)
		s.Done()
		return
	}
	s.Queue()
}

func (s *SegmentRequest) Queue() {
	if s.done || s.failed {
		return
	}

	s.workQueue <- s
}

func (s *SegmentRequest) RequeueIfFailed(server string) bool {
	if val, ok := s.excludedHosts[server]; ok && val {
		log.Printf("Already failed at %s on %s\n", s.MessageID, server)
		s.Queue()
		return true
	}

	return false
}

func NewNZB(nzbfile []byte, jobName string) (*Nzb, error) {
	nzb := Nzb{jobName: jobName, fileRe: regexp.MustCompile(`"(.*)"`)}
	err := xml.Unmarshal(nzbfile, &nzb)
	if err != nil {
		return nil, err
	}

	return &nzb, nil
}

func (n *Nzb) extractFileName(subject string) string {
	submatches := n.fileRe.FindSubmatch([]byte(subject))
	if submatches == nil {
		return ""
	}

	return string(submatches[0])
}

func (n *Nzb) EnqueueFetches(workQueue chan *SegmentRequest, wg *sync.WaitGroup) {
	var size int64

	for _, file := range n.File {
		filename := n.extractFileName(file.Subject)
		if !strings.Contains(strings.ToLower(filename), "par2") {
			log.Println("Skipping", filename)
			//continue
		}

		for _, seg := range file.Segment {
			size += seg.Bytes
			wg.Add(1)
			seg := SegmentRequest{MessageID: seg.MessageID, filename: filename, workGroup: wg, nzb: n, workQueue: workQueue, Bytes: seg.Bytes}
			go seg.Queue()
		}
	}

	bar := pb.New64(size).Prefix(n.jobName + " ")
	bar.ShowSpeed = true
	bar.SetUnits(pb.U_BYTES)
	bar.Start()
	n.progress = bar

}

func (n *Nzb) Complete(bytes int64) {
	n.progress.Add64(bytes)
}
