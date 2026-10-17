package main

import (
	"github.com/aeon022/diaryctl/cmd"
	"github.com/aeon022/missionctl-core/secure"
)

func main() {
	secure.Init("diaryctl") // owner-only files from the first byte
	cmd.Execute()
}
