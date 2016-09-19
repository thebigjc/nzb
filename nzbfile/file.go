package nzbfile

import (
	"encoding/xml"
	"regexp"
	"strings"
	"sync"

	"github.com/cheggaaa/pb"
	"github.com/thebigjc/nzb/yenc"
)

type Nzb struct {
	File   []File `xml:"file"`
	fileRe *regexp.Regexp
}

type File struct {
	Poster  string    `xml:"poster,attr"`
	Date    string    `xml:"date,attr"`
	Subject string    `xml:"subject,attr"`
	Group   []string  `xml:"groups>group"`
	Segment []Segment `xml:"segments>segment"`
}

type Segment struct {
	Bytes     string `xml:"bytes,attr"`
	Number    int    `xml:"number,attr"`
	MessageID string `xml:",chardata"`
}

type SegmentRequest struct {
	group     []string
	MessageID string
	filename  string
	workGroup *sync.WaitGroup
}

var bars map[string]*pb.ProgressBar = make(map[string]*pb.ProgressBar)

func (s *SegmentRequest) Observe(y *yenc.Yenc) {
	defer s.workGroup.Done()

	bar, ok := bars[y.Name]

	if !ok {
		bar = pb.StartNew(y.Size).Prefix(y.Name)
		bar.ShowSpeed = true
		bar.SetUnits(pb.U_BYTES)
		bars[y.Name] = bar
	}

	bar.Add(y.EndSize)
}

func NewNZB(nzbfile []byte) (*Nzb, error) {
	nzb := Nzb{}
	err := xml.Unmarshal(nzbfile, &nzb)
	if err != nil {
		return nil, err
	}

	nzb.fileRe = regexp.MustCompile(`"(.*)"`)

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
			seg := SegmentRequest{group: file.Group, MessageID: seg.MessageID, filename: filename, workGroup: wg}
			go func(seg *SegmentRequest) {
				workQueue <- seg
			}(&seg)
		}
	}
}
