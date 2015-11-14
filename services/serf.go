package services

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func NewSerf(conf SerfConf) (Serf, error) {
	return &serfProcess{
		conf: conf,
	}, nil
}

type Serf interface {
	Start() error
	Stop() error
	Join(addr string) error
	Members() ([]*Member, error)
}

type SerfConf struct {
	Bind string
}

type Member struct{}

//serf process runs serf in a seperate process
//but implements the serf interface
type serfProcess struct {
	conf SerfConf

	*os.Process
}

func (s *serfProcess) Start() error {
	cmd := exec.Command("serf", "agent", fmt.Sprintf("-bind=%s", s.conf.Bind))

	//@todo find more elegant logging solution
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		return err
	}

	//@todo wait for output to container certain string
	s.Process = cmd.Process
	return nil
}

func (s *serfProcess) Join(addr string) error {
	cmd := exec.Command("serf", "join", addr)

	//@todo find more elegant logging solution
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (s *serfProcess) Members() ([]*Member, error) {
	v := struct {
		Members []*Member `json: "members"`
	}{}

	r, w := io.Pipe()
	cmd := exec.Command("serf", "members", "-status=alive", "-format=json")
	cmd.Stderr = os.Stderr
	cmd.Stdout = w
	err := cmd.Start()
	if err != nil {
		return v.Members, err
	}

	dec := json.NewDecoder(r)
	err = dec.Decode(&v)
	if err != nil {
		return v.Members, nil
	}

	return v.Members, nil
}

func (s *serfProcess) Stop() error {
	//@todo cause a gracefull leave instead
	s.Process.Signal(os.Interrupt)

	_, err := s.Process.Wait()
	if err != nil {
		return err
	}

	return nil
}
