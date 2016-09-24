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
	group     []string
	MessageID string
	filename  string
	workGroup *sync.WaitGroup
	nzb       *Nzb
}

func (s *SegmentRequest) BuildWriter(outDir string, filename string) (io.WriterAt, error) {
	dirName := path.Join(outDir, s.nzb.jobName)

	err := os.MkdirAll(dirName, 0777)
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

func (s *SegmentRequest) Observe(count int) {
	s.nzb.Complete(count)
	s.workGroup.Done()
}

func NewNZB(nzbfile []byte, jobName string) (*Nzb, error) {
	nzb := Nzb{jobName: jobName, fileRe: regexp.MustCompile(`"(.*)"`)}
	err := xml.Unmarshal(nzbfile, &nzb)
	if err != nil {
		return nil, err
	}

	var size int64

	for _, file := range nzb.File {
		if strings.Contains(file.Subject, "par2") {
			continue
		}
		for _, seg := range file.Segment {
			size += seg.Bytes
		}
	}

	log.Println("Unmarshall", len(nzb.File), size)

	bar := pb.New64(size).Prefix(jobName + " ")
	bar.ShowSpeed = true
	bar.SetUnits(pb.U_BYTES)
	bar.Start()
	nzb.progress = bar

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
	for _, file := range n.File {
		filename := n.extractFileName(file.Subject)
		if strings.Contains(filename, "par2") {
			continue
		}
		for _, seg := range file.Segment {
			wg.Add(1)
			seg := SegmentRequest{group: file.Group, MessageID: seg.MessageID, filename: filename, workGroup: wg, nzb: n}
			go func(seg *SegmentRequest) {
				workQueue <- seg
			}(&seg)
		}
	}
}

func (n *Nzb) Complete(bytes int) {
	n.progress.Add(bytes)
}
