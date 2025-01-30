package recording

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/slcjordan/autodemo"
	"github.com/slcjordan/autodemo/logger"
)

func findRandomOpenPort() (int, error) {
	listener, err := net.Listen("tcp", ":0") // let OS pick a random open port
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

type Terminal struct {
	conn        net.Conn
	cli         *client.Client
	containerID string
	ctx         context.Context
}

// outfile := fmt.Sprintf("clip-%d.webm", rand.Uint32())
func NewTerminalWithContext(ctx context.Context, outfile string) (*Terminal, error) {

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, err
	}

	port, err := findRandomOpenPort()
	if err != nil {
		return nil, err
	}
	saveDirMount := mount.Mount{
		Type:   mount.TypeBind,
		Source: filepath.Dir(outfile),
		Target: "/recordings",
	}
	resp, err := cli.ContainerCreate(
		ctx,
		&container.Config{
			Image: "autodemo-screen-capture",
			Env: []string{
				fmt.Sprintf("AUTODEMO_LISTEN=0.0.0.0:%d", port),
				fmt.Sprintf("AUTODEMO_OUTFILE=/recordings/%s", filepath.Base(outfile)),
			},
			ExposedPorts: nat.PortSet{
				nat.Port(fmt.Sprintf("%d/tcp", port)): {},
			},
		},
		&container.HostConfig{
			// AutoRemove: true,
			Mounts: []mount.Mount{saveDirMount},
			PortBindings: nat.PortMap{
				nat.Port(fmt.Sprintf("%d/tcp", port)): {
					{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", port)},
				},
			},
		}, nil, nil, "",
	)
	if err != nil {
		return nil, err
	}

	err = cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		return nil, err
	}

	time.Sleep(time.Second)
	var conn net.Conn
	dialer := net.Dialer{
		Timeout:   time.Second,
		KeepAlive: 30 * time.Second,
	}
	for i := 0; i < 10; i++ {
		conn, err = dialer.Dial("tcp", fmt.Sprintf("localhost:%d", port))
		if err != nil {
			retry := time.Second
			logger.Infof(ctx, "could not connect to port %d: %s... retrying in %s", port, err, retry)
			time.Sleep(retry)
			continue
		}
		fmt.Println("connected to", fmt.Sprintf("localhost:%d", port))
		break
	}
	if err != nil {
		return nil, err
	}
	return &Terminal{
		conn:        conn,
		cli:         cli,
		containerID: resp.ID,
		ctx:         ctx,
	}, nil
}

func (t *Terminal) Write(p []byte) (n int, err error) {
	return t.conn.Write(p)
}

func (t *Terminal) Close() error {
	fmt.Println("closing connection")
	var closeErrors []error
	err := t.conn.Close()
	if err != nil {
		fmt.Println(err)
		closeErrors = append(closeErrors, err)
	}
	time.Sleep(time.Second) // add some extra time to the end of the recording.
	/*
		to := 10
		fmt.Println("stopping container")
		err = t.cli.ContainerStop(t.ctx, t.containerID, container.StopOptions{
			Timeout: &to,
		})
		if err != nil {
			closeErrors = append(closeErrors, err)
		}
	*/
	if closeErrors != nil {
		return autodemo.MultiError(closeErrors)
	}
	return nil
}
