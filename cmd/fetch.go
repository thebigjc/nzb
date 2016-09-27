// Copyright © 2016 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"io/ioutil"
	"log"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/thebigjc/nzb/nntp"
	"github.com/thebigjc/nzb/nzbfile"
	"gopkg.in/yaml.v2"
)

type config struct {
	Servers []Server
}

type Server struct {
	Port        int
	Connections int
	Host        string
	Password    string
	Username    string
}

// fetchCmd represents the fetch command
var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "fetch the contents of an nzb file from a news server",
	Long:  `Given an NZB file, fetch the associated files from a Usenet server.`,
	Run: func(cmd *cobra.Command, args []string) {
		usr, _ := user.Current()
		configBytes, err := ioutil.ReadFile(path.Join(usr.HomeDir, ".nzb.yaml"))
		if err != nil {
			log.Fatalln("Error reading config file", err)
		}
		var c config
		err = yaml.Unmarshal(configBytes, &c)
		if err != nil {
			log.Fatalln("Error parsing config file", err)
		}
		workQueue := make(chan *nzbfile.SegmentRequest, 100)

		log.Printf("Found %d servers\n", len(c.Servers))

		// TODO: Cleanup this method

		var wg sync.WaitGroup

		var ns []*nzbfile.Nzb

		for _, s := range c.Servers {
			nntp.BuildWorkers(workQueue, len(c.Servers), s.Connections, s.Port, s.Host, s.Username, s.Password, true, "incomplete")
		}

		for _, arg := range args {
			ns = append(ns, FetchNzbFromFile(arg, workQueue, &wg))
		}

		wg.Wait()

		var parns []*nzbfile.Nzb

		var failedAny bool

		for _, n := range ns {
			log.Printf("Completed %s. %d done, %d failed\n", n.JobName, n.DoneCount, n.FailedCount)
			if n.FailedCount > 0 {
				n.EnqueueFetches(workQueue, true)
				parns = append(parns, n)
				failedAny = true
			}
		}

		wg.Wait()

		if failedAny {
			for _, n := range parns {
				log.Printf("Completed PAR2 %s. %d done, %d failed\n", n.JobName, n.DoneCount, n.FailedCount)
			}
		}
	},
}

func FetchNzbFromFile(filename string, workQueue chan *nzbfile.SegmentRequest, wg *sync.WaitGroup) *nzbfile.Nzb {
	nzbBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading from %s: %v\n", filename, err)
	}

	basename := path.Base(filename)
	name := strings.TrimSuffix(basename, filepath.Ext(basename))

	nzbFile, err := nzbfile.NewNZB(nzbBytes, name, wg)
	if err != nil {
		log.Fatalf("Error parsing %s: %v\n", filename, err)
	}

	nzbFile.EnqueueFetches(workQueue, false)

	return nzbFile
}

func init() {
	RootCmd.AddCommand(fetchCmd)
}
