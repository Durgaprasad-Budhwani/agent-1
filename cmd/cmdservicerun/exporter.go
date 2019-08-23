package cmdservicerun

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/pinpt/agent.next/cmd/cmdupload"

	"github.com/pinpt/agent.next/pkg/fsconf"

	"github.com/hashicorp/go-hclog"
	"github.com/pinpt/agent.next/cmd/cmdexport"
	"github.com/pinpt/integration-sdk/agent"
)

type exporterOpts struct {
	Logger       hclog.Logger
	CustomerID   string
	PinpointRoot string
	FSConf       fsconf.Locs

	PPEncryptionKey string
}

type exporter struct {
	ExportQueue chan exportRequest

	logger hclog.Logger
	opts   exporterOpts
}

type exportRequest struct {
	Done chan error
	Data *agent.ExportRequest
}

func newExporter(opts exporterOpts) *exporter {
	if opts.PPEncryptionKey == "" {
		panic(`opts.PPEncryptionKey == ""`)
	}
	s := &exporter{}
	s.opts = opts
	s.logger = opts.Logger
	s.ExportQueue = make(chan exportRequest)
	return s
}

func (s *exporter) Run() {
	for req := range s.ExportQueue {
		req.Done <- s.export(req.Data)
	}
	return
}

func (s *exporter) export(data *agent.ExportRequest) error {
	s.logger.Info("processing export request", "upload_url", *data.UploadURL, "reprocess_historical", data.ReprocessHistorical)

	agentConfig := cmdexport.AgentConfig{}
	agentConfig.CustomerID = s.opts.CustomerID
	agentConfig.PinpointRoot = s.opts.PinpointRoot

	var integrations []cmdexport.Integration

	/*
		integrations = append(integrations, cmdexport.Integration{
			Name:   "mock",
			Config: map[string]interface{}{"k1": "v1"},
		})
	*/

	for _, integration := range data.Integrations {

		s.logger.Info("exporting integration", "name", integration.Name, "len(exclusions)", len(integration.Exclusions))

		conf, err := configFromEvent(integration.ToMap(), s.opts.PPEncryptionKey)
		if err != nil {
			return err
		}

		integrations = append(integrations, conf)
	}

	ctx := context.Background()

	fsconf := s.opts.FSConf

	// delete existing uploads
	err := os.RemoveAll(fsconf.Uploads)
	if err != nil {
		return err
	}

	err = s.execExport(ctx, agentConfig, integrations, data.ReprocessHistorical)
	if err != nil {
		return err
	}

	s.logger.Info("export finished, running upload")

	err = cmdupload.Run(ctx, s.logger, s.opts.PinpointRoot, *data.UploadURL)
	if err != nil {
		return err
	}
	return nil
}

func (s *exporter) execExport(ctx context.Context, agentConfig cmdexport.AgentConfig, integrations []cmdexport.Integration, reprocessHistorical bool) error {
	args := []string{"export"}
	if reprocessHistorical {
		args = append(args, "--reprocess-historical=true")
	}

	fs, err := newFsPassedParams(s.opts.FSConf.Temp, []kv{
		{"--agent-config-file", agentConfig},
		{"--integrations-file", integrations},
	})
	if err != nil {
		return err
	}
	args = append(args, fs.Args()...)
	defer fs.Clean()

	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type kv struct {
	K string
	V interface{}
}

type fsPassedParams struct {
	args    []kv
	tempDir string
	files   []string
}

func newFsPassedParams(tempDir string, args []kv) (*fsPassedParams, error) {
	s := &fsPassedParams{}
	s.args = args
	s.tempDir = tempDir
	for _, arg := range args {
		loc, err := s.writeFile(arg.V)
		if err != nil {
			return nil, err
		}
		s.files = append(s.files, loc)
	}
	return s, nil
}

func (s *fsPassedParams) writeFile(obj interface{}) (string, error) {
	err := os.MkdirAll(s.tempDir, 0777)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	f, err := ioutil.TempFile(s.tempDir, "")
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.Write(b)
	if err != nil {
		return "", err
	}
	return f.Name(), nil
}

func (s *fsPassedParams) Args() (res []string) {
	for i, kv0 := range s.args {
		k := kv0.K
		v := s.files[i]
		res = append(res, k, v)
	}
	return
}

func (s *fsPassedParams) Clean() error {
	for _, f := range s.files {
		err := os.Remove(f)
		if err != nil {
			return err
		}
	}
	return nil
}
