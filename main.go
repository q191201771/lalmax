package main

import (
	"context"
	"flag"
	"fmt"
	"lalmax/srt"
	"os"

	"github.com/q191201771/naza/pkg/nazalog"

	"github.com/q191201771/lal/pkg/base"

	config "lalmax/conf"

	"github.com/q191201771/lal/pkg/logic"
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

	lals := logic.NewLalServer(func(option *logic.Option) {
		option.ConfFilename = maxConf.LalSvrConfigPath
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if maxConf.SrtConfig.Enable {
		go func() {
			srtSvr := srt.NewSrtServer(maxConf.SrtConfig, lals)
			srtSvr.Run(ctx)
		}()
	}

	err = lals.RunLoop()
	nazalog.Infof("server manager done. err=%+v", err)
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

	return *cf
}
