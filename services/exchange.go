package services

import (
	"net/url"
	"os"
	"os/exec"
)

func NewDeluge() (Exchange, error) {
	return &delugeProcess{}, nil
}

type Exchange interface {
	Start() error
	Stop() error
	Pull(id string) error
}

type delugeProcess struct {
	*os.Process
}

func (d *delugeProcess) Start() error {
	cmd := exec.Command("deluged", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		return err
	}

	d.Process = cmd.Process
	return nil
}

func (d *delugeProcess) Pull(uri string) error {
	loc, err := url.Parse(uri)
	if err != nil {
		return err
	}

	cmd := exec.Command("deluge-console", "add", loc.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func (d *delugeProcess) Stop() error {
	_, err := d.Process.Wait()
	if err != nil {
		return err
	}

	return nil
}
