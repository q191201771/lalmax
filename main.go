package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/q191201771/lalmax/server"

	"github.com/q191201771/naza/pkg/nazalog"

	"github.com/q191201771/lal/pkg/base"

	config "github.com/q191201771/lalmax/conf"

	"github.com/q191201771/naza/pkg/bininfo"
)

func main() {
	defer nazalog.Sync()

	confFilename := parseFlag()
	err := config.Open(confFilename)
	if err != nil {
		nazalog.Errorf("open config failed, configname:%+v", confFilename)
		return
	}

	maxConf := config.GetConfig()

	svr, err := server.NewLalMaxServer(maxConf)
	if err != nil {
		nazalog.Fatalf("create lalmax server failed. err=%+v", err)
	}

	if err = svr.Run(); err != nil {
		nazalog.Infof("server manager done. err=%+v", err)
	}
}

func parseFlag() string {
	binInfoFlag := flag.Bool("v", false, "show bin info")
	cf := flag.String("c", "", "specify conf file")
	p := flag.String("p", "", "specify current work directory")
	flag.Parse()

	if *binInfoFlag {
		_, _ = fmt.Fprint(os.Stderr, bininfo.StringifyMultiLine())
		_, _ = fmt.Fprintln(os.Stderr, base.LalFullInfo)
		os.Exit(0)
	}
	if *p != "" {
		os.Chdir(*p)
	}
	if *cf != "" {
		return *cf
	}
	nazalog.Warnf("config file did not specify in the command line, try to load it in the usual path.")
	defaultConfigFileList := []string{
		filepath.FromSlash("lalmax.conf.json"),
		filepath.FromSlash("./conf/lalmax.conf.json"),
		filepath.FromSlash("../conf/lalmax.conf.json"),
	}
	for _, dcf := range defaultConfigFileList {
		fi, err := os.Stat(dcf)
		if err == nil && fi.Size() > 0 && !fi.IsDir() {
			nazalog.Warnf("%s exist. using it as config file.", dcf)
			return dcf
		} else {
			nazalog.Warnf("%s not exist.", dcf)
		}
	}

	// 默认位置都没有，退出程序
	flag.Usage()
	_, _ = fmt.Fprintf(os.Stderr, `
						Example:
						  %s -c %s
						`, os.Args[0], filepath.FromSlash("./conf/lalmax.conf.json"))
	base.OsExitAndWaitPressIfWindows(1)
	return *cf
}
