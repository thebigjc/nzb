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
	"github.com/spf13/viper"
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
		log.Println("fetch called")
		workQueue := make(chan *nzbfile.SegmentRequest, 100)
		var wg sync.WaitGroup

		log.Printf("Found %d servers\n", len(c.Servers))

		for _, s := range c.Servers {
			log.Println(s)
			nntp.BuildWorkers(workQueue, s.Connections, s.Port, s.Host, s.Username, s.Password, true, "incomplete")
		}

		for _, arg := range args {
			FetchNzbFromFile(arg, workQueue, &wg)
		}
		wg.Wait()
	},
}

func FetchNzbFromFile(filename string, workQueue chan *nzbfile.SegmentRequest, wg *sync.WaitGroup) {
	nzbBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading from %s: %v\n", filename, err)
	}

	basename := path.Base(filename)
	name := strings.TrimSuffix(basename, filepath.Ext(basename))

	nzbFile, err := nzbfile.NewNZB(nzbBytes, name)
	if err != nil {
		log.Fatalf("Error parsing %s: %v\n", filename, err)
	}

	nzbFile.EnqueueFetches(workQueue, wg)
}

func init() {
	RootCmd.AddCommand(fetchCmd)

	flags := fetchCmd.Flags()

	flagNames := []string{"host", "port", "username", "password", "use-tls", "conn", "incomplete"}

	flags.String("host", "", "The newsreader host to download from")
	flags.Int("port", 0, "The newsreader port to download from")
	flags.Int("conn", 10, "The number of connections to open")
	flags.String("username", "", "The user to login as")
	flags.String("password", "", "The password to login with")
	flags.String("incomplete", "incomplete", "The directory to store downloads in")
	flags.Bool("use-tls", true, "Whether to use TLS to talk to the server")

	for _, flag := range flagNames {
		viper.BindPFlag(flag, flags.Lookup(flag))
	}
}
