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
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/thebigjc/nzb/nntp"
	"github.com/thebigjc/nzb/nzbfile"
)

type config struct {
	Port     int
	Host     string
	Username string
	Password string
	Conn     int
	UseTLS   bool `mapstructure:"use-tls"`
}

// fetchCmd represents the fetch command
var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "fetch the contents of an nzb file from a news server",
	Long:  `Given an NZB file, fetch the associated files from a Usenet server.`,
	Run: func(cmd *cobra.Command, args []string) {
		var c config
		viper.Unmarshal(&c)
		log.Println("fetch called")
		workQueue := make(chan *nzbfile.SegmentRequest, 100)
		var wg sync.WaitGroup

		nntp.BuildNNTPWorkers(workQueue, c.Conn, c.Port, c.Host, c.Username, c.Password, c.UseTLS)
		// Validate config here
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

	nzbFile, err := nzbfile.NewNZB(nzbBytes)
	if err != nil {
		log.Fatalf("Error parsing %s: %v\n", filename, err)
	}

	nzbFile.EnqueueFetches(workQueue, wg)
}

func init() {
	RootCmd.AddCommand(fetchCmd)

	flags := fetchCmd.Flags()

	flagNames := []string{"host", "port", "username", "password", "use-tls", "conn"}

	flags.String("host", "", "The newsreader host to download from")
	flags.Int("port", 0, "The newsreader port to download from")
	flags.Int("conn", 10, "The number of connections to open")
	flags.String("username", "", "The user to login as")
	flags.String("password", "", "The password to login with")
	flags.Bool("use-tls", true, "Whether to use TLS to talk to the server")

	for _, flag := range flagNames {
		viper.BindPFlag(flag, flags.Lookup(flag))
	}
}
