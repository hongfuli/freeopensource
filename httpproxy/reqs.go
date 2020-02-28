package httpproxy

import (
	"bufio"
	"fmt"
	"github.com/hongfuli/freeopensource/log"
	"io"
	"net"
	"strconv"
	"sync"
)

var logger = log.GetLogger()

func StartUP() {
	var (
		serverFd net.Listener
		clientFd net.Conn
		err      error
	)

	// 启动监控端口提供代理服务
	logger.Info("starting server ...")
	if serverFd, err = net.Listen("tcp", ":8000"); err != nil {
		logger.Fatalf("open http proxy server fail: %v", err)
	}
	logger.Info("success started server ...")

	// 死循环接收代理请求
	for {
		if clientFd, err = serverFd.Accept(); err != nil {
			logger.Fatalf("receive connection fail, exit server: %v", err)
		}
		// 新协程中并行处理客户端请求
		go process(clientFd)
	}

}

// 处理客户端请求
func process(clientFd net.Conn) (err error) {
	logger.Info("receive from client conn: %s", clientFd.RemoteAddr())
	var (
		req              *request
		client           *bufio.ReadWriter
		remoteFd         net.Conn
		remote           *bufio.ReadWriter
		remoteContentLen int
		closeFlag        sync.WaitGroup
	)
	// 用buf封装底层socket连接，方便读写网络操作
	client = bufio.NewReadWriter(bufio.NewReader(clientFd), bufio.NewWriter(clientFd))
	// 构建req实例
	if req, err = buildReq(client.Reader); err != nil {
		return fmt.Errorf("build client request fail for first line: %s", err)
	}
	logger.Debugf("build request: %+v", req)
	// 读取客户端headers
	var clientHeaders []*header
	if clientHeaders, err = readHeaders(client.Reader); err != nil {
		return fmt.Errorf("read client headers fail: %s", err)
	}

	// 建立目标服务网络连接
	if remoteFd, err = net.Dial("tcp", fmt.Sprintf("%s:%d", req.host, req.port)); err != nil {
		return fmt.Errorf("connect remote server [%s:%d] fail: %s", req.host, req.port, err)
	}
	logger.Infof("success connected remote server: %s", remoteFd.RemoteAddr())
	// 封装buf方便网络数据读写操作
	remote = bufio.NewReadWriter(bufio.NewReader(remoteFd), bufio.NewWriter(remoteFd))

	if req.method == "CONNECT" {
		// https 请求需要升级连接通道
		if err = responseConnectReq(client.Writer, req); err != nil {
			return fmt.Errorf("response for CONNECT fail: %s", err)
		}
	} else {
		// 写入http协议首行到目标服务
		if err = writeRemoteReqFirstLine(remote.Writer, req); err != nil {
			return fmt.Errorf("write first line to remote server fail")
		}
		// 把客户端请求头部转写目标服务
		if err = writeHeaders(remote.Writer, clientHeaders); err != nil {
			fmt.Errorf("wirte remote request headers fail: %s", err)
		}
		// 如果客户端请求头中包含Content-Length头，则转发body内容到目标服务
		if err = transferFixedRequestBody(client.Reader, remote.Writer, clientHeaders); err != nil {
			return fmt.Errorf("transfer client request content body fail: %s", err)
		}
		// 读取目标服务返回首行，并转写入客户端
		if remoteRespFirstLine, err := readLine(remote.Reader); err != nil {
			return fmt.Errorf("read remote first line fail: %s", err)
		} else {
			logger.Debugf("write first line to client response: %s", remoteRespFirstLine)
			client.WriteString(remoteRespFirstLine + "\r\n")
		}
		if err = client.Flush(); err != nil {
			return fmt.Errorf("wirite to client fail: %s", err)
		}

		// 读取目标服务返回headers
		remoteRespHeaders, err := readHeaders(remote.Reader)
		if err != nil {
			return fmt.Errorf("read headers from remote fail: %s", err)
		}
		// 将目标服务返回的header写入客户端
		if err = writeHeaders(client.Writer, remoteRespHeaders); err != nil {
			return fmt.Errorf("write response headers to client fail: %s", err)
		}

		//
		for _, header := range remoteRespHeaders {
			if header.name == "Content-Length" {
				remoteContentLen, err = strconv.Atoi(header.value)
				if err != nil {
					return fmt.Errorf("remote content lenght value invalid: %s", header.value)
				}
				break
			}
		}
	}

	closeFlag.Add(2)
	// 中转客户端和目标服务端两个方向的数据
	relay(remoteFd, client, remote, remoteContentLen, &closeFlag)
	closeFlag.Wait()
	// 关闭连接
	clientFd.Close()
	remoteFd.Close()
	logger.Infof("closed client conn: %s", clientFd.RemoteAddr())
	logger.Infof("closed remote conn: %s", remoteFd.RemoteAddr())
	return nil
}

// 客户端 - 代理服务 - 目标服务，代理服务做为中间服务，从两个方向中转传输数据
func relay(proxyFd net.Conn, client *bufio.ReadWriter, remote *bufio.ReadWriter, remoteDataSize int, closeFlag *sync.WaitGroup) (err error) {
	go func() {
		if err = transferData(client.Reader, remote.Writer, 0); err != nil && err != io.EOF {
			logger.Errorf("transfer data from client to remote fail: %s", err)
		}
		logger.Debug("transferred all data from client")
		if err == io.EOF {
			// 客户端已主动关闭连接，所以可以关闭远程连接
			proxyFd.Close()
		}
		closeFlag.Done()
	}()

	go func() {
		if err = transferData(remote.Reader, client.Writer, remoteDataSize); err != nil && err != io.EOF {
			logger.Errorf("transfer data from remote to client fail: %s", err)
		}
		logger.Debug("transferred all data from remote to client")
		closeFlag.Done()
	}()
	return
}
