package terraform

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mattermost/mattermost-load-test/ltops"

	"github.com/mattermost/mattermost-load-test/sshtools"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func (c *Cluster) loadtestInstance(addr string, configFile []byte, resultsOutput io.Writer) error {
	client, err := sshtools.SSHClient(c.SSHKey(), addr)
	if err != nil {
		return errors.Wrap(err, "unable to connect to loadtest instance via ssh")
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return errors.Wrap(err, "unable to create ssh session")
	}
	defer session.Close()

	if len(configFile) > 0 {
		if err := sshtools.UploadBytes(client, configFile, "mattermost-load-test/loadtestconfig.json"); err != nil {
			return errors.Wrap(err, "failed to upload config file")
		}
	}

	commandOutputFile := filepath.Join(c.Env.WorkingDirectory, "results", "loadtest-out-"+addr+".txt")
	if err := os.MkdirAll(filepath.Dir(commandOutputFile), 0700); err != nil {
		return errors.Wrap(err, "Unable to create results directory.")
	}
	outfile, err := os.Create(commandOutputFile)
	if err != nil {
		return errors.Wrap(err, "Unable to create loadtest results file.")
	}

	if resultsOutput != nil {
		session.Stdout = io.MultiWriter(outfile, resultsOutput)
	} else {
		session.Stdout = outfile
	}
	session.Stderr = outfile

	logrus.Info("Running loadtest on " + addr)
	if err := session.Run("cd mattermost-load-test && ./bin/loadtest all"); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) Loadtest(options *ltops.LoadTestOptions) error {
	loadtestInstancesAddrs, err := c.GetLoadtestInstancesAddrs()
	if err != nil || len(loadtestInstancesAddrs) <= 0 {
		return errors.Wrap(err, "Unable to get loadtest instance addresses")
	}

	var wg sync.WaitGroup
	wg.Add(len(loadtestInstancesAddrs))

	var configFile []byte
	if len(options.ConfigFile) > 0 {
		data, err := ltops.GetFileOrURL(options.ConfigFile)
		if err != nil {
			return errors.Wrap(err, "failed to load config file")
		}

		configFile = data
	}

	for i, addr := range loadtestInstancesAddrs {
		addr := addr
		go func() {
			var err error
			if i == 0 {
				err = c.loadtestInstance(addr, configFile, options.ResultsWriter)
			} else {
				err = c.loadtestInstance(addr, configFile, nil)
			}
			if err != nil {
				logrus.Error(err)
			}
			wg.Done()
		}()
		// Give some time between instances just to avoid any races
		time.Sleep(time.Second * 10)
	}

	logrus.Info("Wating for loadtests to complete. See: " + filepath.Join(c.Env.WorkingDirectory, "results") + " for results.")
	wg.Wait()

	return nil
}
