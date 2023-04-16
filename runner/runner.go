// Copyright 2022 Dhi Aurrahman
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runner

import (
	"bufio"
	"context"
	_ "embed" // to allow embedding files
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/nats-io/nats.go"
)

// New returns a new runner.
func New(binaryPath string, silence bool, natsConn *nats.Conn, envoyAdminPort int, nodeID string) *Runner {
	return &Runner{
		binaryPath:     binaryPath,
		silence:        silence,
		natsConn:       natsConn,
		publishLogs:    false,
		envoyAdminPort: envoyAdminPort,
		nodeID:         nodeID,
	}
}

// Runner runs proxy at binary path.
type Runner struct {
	binaryPath     string
	silence        bool
	natsConn       *nats.Conn
	publishLogs    bool
	envoyAdminPort int
	nodeID         string
}

func processStream(reader io.Reader, subject string, nc *nats.Conn, stream string, shouldPublish func() bool) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if stream == "stdout" {
			fmt.Println(line)
		} else {
			fmt.Fprintln(os.Stderr, line)
		}

		if shouldPublish() {
			nc.Publish(subject, []byte(line))
		}
	}
}

// Run runs proxy with the specified arguments.
func (r *Runner) Run(ctx context.Context, args []string) error {
	// We don't use CommandContext here so we can send exactly SIGTERM instead of kill -9 or SIGINT
	// when killing the process.
	cmd := exec.Command(filepath.Clean(r.binaryPath), args...) //nolint:gosec

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}

	starter := newStarter(cmd)

	err = starter.Start(ctx)
	if err != nil {
		return err
	}

	logStreamSubject := fmt.Sprintf("PROXY.%s.logs", r.nodeID)
	var wg sync.WaitGroup
	wg.Add(2)

	shouldPublish := func() bool {
		return r.publishLogs
	}

	// Publish standard output to the "stdout" subject.
	go func() {
		processStream(stdoutPipe, logStreamSubject, r.natsConn, "stdout", shouldPublish)
		wg.Done()
	}()

	// Publish standard error to the "stderr" subject.
	go func() {
		processStream(stderrPipe, logStreamSubject, r.natsConn, "stderr", shouldPublish)
		wg.Done()
	}()

	// admin interface, path are inside msg.Data
	adminSubject := fmt.Sprintf("PROXY.%s.admin", r.nodeID)
	adminQueueName := fmt.Sprintf("NATS_REPLY_ADMIN_INTERFACE_%s", r.nodeID)
	r.natsConn.QueueSubscribe(adminSubject, adminQueueName, func(msg *nats.Msg) {
		// send request to admin path
		path := string(msg.Data)
		res, err := http.Get(fmt.Sprintf("http://localhost:%d/%s", r.envoyAdminPort, path))
		if err != nil {
			log.Println(err)
		}
		defer res.Body.Close()
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			log.Println(err)
		}
		msg.Respond(body)

	})

	logSubject := fmt.Sprintf("PROXY.%s.publish_logs", r.nodeID)
	logQueueName := fmt.Sprintf("NATS_REPLY_LOGS_COMMAND_%s", r.nodeID)
	r.natsConn.QueueSubscribe(logSubject, logQueueName, func(msg *nats.Msg) {
		state := string(msg.Data)
		if state == "start" {
			r.publishLogs = true
		} else {
			r.publishLogs = false
		}
		msg.Respond([]byte("ok"))
	})
	r.natsConn.Flush()

	if err := r.natsConn.LastError(); err != nil {
		log.Println(err)
	}

	// TODO(dio): Do checking on admin address path availability, so we know that the process is
	// ready.

	go func() {
		<-ctx.Done()
		_ = starter.Kill()
	}()

	wg.Wait()
	err = starter.Wait()
	if err != nil {
		return err
	}

	if cmd.Process != nil {
		return cmd.Process.Kill()
	}
	return nil
}
