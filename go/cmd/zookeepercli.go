/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"github.com/outbrain/golib/log"
	"github.com/outbrain/zookeepercli/go/output"
	"github.com/outbrain/zookeepercli/go/zk"
	"io/ioutil"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"
)

// main is the application's entry point.
func main() {
	servers := flag.String("servers", "", "srv1[:port1][,srv2[:port2]...]")
	command := flag.String("c", "", "command, required (exists|get|ls|lsr|create|creater|set|delete|rm|deleter|rmr|getacl|setacl)")
	force := flag.Bool("force", false, "force operation")
	format := flag.String("format", "txt", "output format (txt|json)")
	omitNewline := flag.Bool("n", false, "omit trailing newline with get in txt format")
	verbose := flag.Bool("verbose", false, "verbose")
	debug := flag.Bool("debug", false, "debug mode (very verbose)")
	stack := flag.Bool("stack", false, "add stack trace upon error")
	authUser := flag.String("auth_usr", "", "optional, digest scheme, user")
	authPwd := flag.String("auth_pwd", "", "optional, digest scheme, pwd")
	acls := flag.String("acls", "31", "optional, csv list [1|,2|,4|,8|,16|,31]")
	concurrentRequests := flag.Int("concurrent_requests", 1, "Number of requests to make in parallel (results in faster lsr)")

	flag.Parse()

	log.SetLevel(log.ERROR)
	if *verbose {
		log.SetLevel(log.INFO)
	}
	if *debug {
		log.SetLevel(log.DEBUG)
	}
	if *stack {
		log.SetPrintStackTrace(*stack)
	}

	if *omitNewline && *format != "txt" {
		log.Fatalf("-n only valid for -format=txt")
	}
	var out output.Printer
	switch *format {
	case "txt":
		out = &output.TxtPrinter{*omitNewline}
	case "json":
		out = &output.JSONPrinter{}
	default:
		log.Fatalf("Unknown output type %q", *format)
	}

	log.Info("starting")

	if *servers == "" {
		log.Fatal("Expected comma delimited list of servers via --servers")
	}
	serversArray := strings.Split(*servers, ",")
	if len(serversArray) == 0 {
		log.Fatal("Expected comma delimited list of servers via --servers")
	}

	if len(*command) == 0 {
		log.Fatal("Expected command (-c) (exists|get|ls|lsr|create|creater|set|delete|rm|deleter|rmr|getacl|setacl)")
	}

	if len(flag.Args()) < 1 {
		log.Fatal("Expected path argument")
	}
	path := flag.Arg(0)

	if strings.HasSuffix(path, "/") {
		if (*command == "ls" || *command == "lsr") && path == "/" {
			// ls'ing on / is fine.  Do nothing
		} else {
			log.Fatal("Path must not end with '/'")
		}
	}

	rand.Seed(time.Now().UnixNano())
	zook := zk.NewZooKeeper()
	zook.SetServers(serversArray)

	if *authUser != "" && *authPwd != "" {
		authExp := fmt.Sprint(*authUser, ":", *authPwd)
		zook.SetAuth("digest", []byte(authExp))
	}

	if *command == "creater" {
		*command = "create"
		*force = true
	}
	switch *command {
	case "exists":
		{
			if exists, err := zook.Exists(path); err == nil && exists {
				out.PrintString([]byte("true"))
			} else {
				log.Fatale(err)
			}
		}
	case "get":
		{
			if result, err := zook.Get(path); err == nil {
				out.PrintString(result)
			} else {
				log.Fatale(err)
			}
		}
	case "getacl":
		{
			if result, err := zook.GetACL(path); err == nil {
				out.PrintStringArray(result)
			} else {
				log.Fatale(err)
			}
		}
	case "ls":
		{
			if result, err := zook.Children(path); err == nil {
				if *format == "txt" {
					sort.Strings(result)
				}
				out.PrintStringArray(result)
			} else {
				log.Fatale(err)
			}
		}
	case "lsr":
		{
			if result, err := zook.ChildrenRecursive(path, *concurrentRequests); err == nil {
				if *format == "txt" {
					sort.Strings(result)
				}
				out.PrintStringArray(result)
			} else {
				log.Fatale(err)
			}
		}
	case "create":
		{
			var aclstr string

			if len(flag.Args()) < 2 {
				log.Fatal("Expected data argument")
			}

			if len(flag.Args()) >= 3 {
				aclstr = flag.Arg(2)
			}

			if *authUser != "" && *authPwd != "" {
				perms, err := zook.BuildACL("digest", *authUser, *authPwd, *acls)
				if err != nil {
					log.Fatale(err)
				}
				if result, err := zook.CreateWithACL(path, []byte(flag.Arg(1)), *force, perms); err == nil {
					log.Infof("Created %+v", result)
				} else {
					log.Fatale(err)
				}
			} else {
				if result, err := zook.Create(path, []byte(flag.Arg(1)), aclstr, *force); err == nil {
					log.Infof("Created %+v", result)
				} else {
					log.Fatale(err)
				}
			}
		}
	case "set":
		{
			var info []byte
			if len(flag.Args()) > 1 {
				info = []byte(flag.Arg(1))
			} else {
				var err error
				info, err = ioutil.ReadAll(os.Stdin)
				if err != nil {
					log.Fatale(err)
				}
			}
			if result, err := zook.Set(path, info); err == nil {
				log.Infof("Set %+v", result)
			} else {
				log.Fatale(err)
			}
		}
	case "setacl":
		{
			var aclstr string
			if len(flag.Args()) > 1 {
				aclstr = flag.Arg(1)
			} else {
				var err error
				data, err := ioutil.ReadAll(os.Stdin)
				aclstr = string(data)
				if err != nil {
					log.Fatale(err)
				}
			}
			if result, err := zook.SetACL(path, aclstr, *force); err == nil {
				log.Infof("Set %+v", result)
			} else {
				log.Fatale(err)
			}
		}
	case "delete", "rm":
		{
			if err := zook.Delete(path); err != nil {
				log.Fatale(err)
			}
		}
	case "deleter", "rmr":
		{
			if !(*force) {
				log.Fatal("deleter (recursive) command requires --force for safety measure")
			}
			if err := zook.DeleteRecursive(path, *concurrentRequests); err != nil {
				log.Fatale(err)
			}
		}
	default:
		log.Fatalf("Unknown command: %s", *command)
	}
}
