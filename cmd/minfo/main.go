// Package main 提供 minfo 服务的命令行启动入口。

package main

import (
	"log"

	"minfo"
	"minfo/internal/app"
	"minfo/internal/version"
)

// main 会启动命令行入口，并把应用运行在当前进程中。
func main() {
	server, err := app.NewServer(minfo.EmbeddedWebUI())
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("minfo %s listening on http://localhost%s", version.Version, server.Addr)
	log.Fatal(server.ListenAndServe())
}
