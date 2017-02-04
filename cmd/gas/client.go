package main

import (
	"fmt"
	"log"
	"net/rpc"
	"os"
	"os/user"
	"strings"
	"text/tabwriter"
)

func handleCommand(name string, args []string) {
	c, err := userConfig(user.Current())
	if err != nil {
		log.Fatal(err)
	}

	rpcArgs := &Args{}
	if len(args) >= 1 {
		rpcArgs.Name = args[0]

		if len(args) >= 2 {
			rpcArgs.Args = args[1:]
		}
	}
	name = "TaskList." + strings.Title(strings.ToLower(name))

	client, err := rpc.Dial("unix", c.sockPath)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	resp := Response{}
	err = client.Call(name, rpcArgs, &resp)
	if err != nil {
		log.Fatal(err)
	}

	if resp.Status != "" {
		fmt.Println(resp.Status)
	}
	if resp.Tasks != nil {
		tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		fmt.Fprintln(tw, "  NAME\tPID\tPORT\tUPTIME")
		for _, task := range resp.Tasks {
			fmt.Fprintln(tw, task)
		}
		tw.Flush()
	}
}
