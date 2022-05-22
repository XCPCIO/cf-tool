package cmd

import (
	"time"

	"github.com/Dup4/cf-tool/client"
	"github.com/Dup4/cf-tool/config"
)

// Race command
func Race() (err error) {
	cfg := config.Instance
	cln := client.Instance
	info := Args.Info

	if err = cln.RaceContest(info); err != nil {
		if err = loginAgain(cln, err); err == nil {
			err = cln.RaceContest(info)
		}
	}

	if err != nil {
		return
	}

	time.Sleep(time.Microsecond)
	URL, err := info.ProblemSetURL(cfg.Host)
	if err != nil {
		return
	}

	openURL(URL)
	openURL(URL + "/problems")

	return Parse()
}
