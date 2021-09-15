package httpproxy

import (
    "bufio"
    "fmt"
    "io"
    "net/url"
    "strconv"
    "strings"
)

var newLineFlag = "\r\n"

type request struct {
    method   string // GET, POST, CONNECT, ...
    scheme   string // http, https, ws
    host     string // localhost
    port     int    // 8888, default 80
    path     string
    query    string // q=a&p=b
    protocol string // HTTP/1.1
}

type header struct {
    name  string
    value string
}

// 读取一行数据，以 "\r\n" 结尾为行结束标志
func readLine(reader *bufio.Reader) (string, error) {
    var (
        line string
        err  error
    )
    if line, err = reader.ReadString('\n'); err != nil {
        return line, fmt.Errorf("read line string fail: %s", err)
    } else {
        if len(line) < 2 {
            return line, fmt.Errorf("line is not ends with CRLF: %s", err)
        }
        if strings.HasSuffix(line, newLineFlag) {
            return line[:len(line)-2], nil
        } else {
            return line, fmt.Errorf("line is not ends with CRLF: %s", err)
        }
    }
}

// 根据客户端请求首行构建request实例
func buildReq(client *bufio.Reader) (*request, error) {
    var (
        firstLine string
        err       error
    )
    if firstLine, err = readLine(client); err != nil {
        return nil, err
    }
    if req, err := parseRequestLine(firstLine); err != nil {
        return nil, err
    } else {
        return req, nil
    }
}

// 当请求为 CONNECT 连接时，需要先反馈写回特定头，然后客户端升级通道
func responseConnectReq(client *bufio.Writer, req *request) (err error) {
    if _, err = client.WriteString(fmt.Sprintf("%s 200 Connection established\r\n", req.protocol)); err != nil {
        return
    }
    if _, err = client.WriteString("\r\n"); err != nil {
        return
    }
    return client.Flush()
}

// 对目标服务写入请求首行
func writeRemoteReqFirstLine(writer *bufio.Writer, req *request) (err error) {
    path := req.path
    if path == "" {
        path = "/"
    }
    if req.query != "" {
        path += "?" + req.query
    }
    line := fmt.Sprintf("%s %s %s\r\n", req.method, path, req.protocol)
    logger.Debugf("write remote first line: %s", line)
    if _, err = writer.WriteString(line); err != nil {
        return
    }
    return
}

// 从 source 端读取数据，写到 dest 端
func transferData(source *bufio.Reader, dest *bufio.Writer, size int) error {
    var (
        buf               = make([]byte, 1024*8)
        totalBytes, rn    int
        readErr, writeErr error
    )
    for {
        rn, readErr = source.Read(buf)
        totalBytes += rn
        if readErr != nil && readErr != io.EOF {
            return readErr
        }
        if rn > 0 {
            _, writeErr = dest.Write(buf[:rn])
            if writeErr != nil {
                return writeErr
            }
            if writeErr = dest.Flush(); writeErr != nil {
                return writeErr
            }
        }
        if size != 0 && totalBytes >= size {
            break
        }
        if readErr == io.EOF {
            return readErr
        }
    }
    return nil
}

// 转发固定长度数据
func transferFixedRequestBody(reader *bufio.Reader, writer *bufio.Writer, headers []*header) (err error) {
    for _, header := range headers {
        if header.name == "Content-Length" {
            length, err := strconv.Atoi(header.value)
            if err != nil {
                return err
            }
            buf := make([]byte, length)
            if _, err = io.ReadFull(reader, buf); err != nil {
                return err
            }
            if _, err = writer.Write(buf); err != nil {
                return err
            }
            return writer.Flush()
        }
    }
    return
}

func writeHeaders(writer *bufio.Writer, headers []*header) (err error) {
    for _, header := range headers {
        if _, err = writer.WriteString(fmt.Sprintf("%s: %s\r\n", header.name, header.value)); err != nil {
            return
        }
    }
    if _, err = writer.WriteString("\r\n"); err != nil {
        return
    }
    return writer.Flush()
}

/*
parse GET http://www.x.com:9999/api/?q=1 HTTP/1.1
*/
func parseRequestLine(requestLine string) (*request, error) {
    var (
        rawUrl *url.URL
        host   string
        port   int
        err    error
    )
    slices := strings.Split(requestLine, " ")
    if len(slices) < 3 {
        return nil, fmt.Errorf("request line invalid: %s", requestLine)
    }

    if slices[0] == "CONNECT" {
        if host, port, err = parseHost(slices[1]); err != nil {
            return nil, fmt.Errorf("host invalid: %s", err)
        }
        return &request{
            method:   slices[0],
            scheme:   "",
            host:     host,
            port:     port,
            path:     "",
            query:    "",
            protocol: slices[2],
        }, nil
    }

    if rawUrl, err = url.Parse(slices[1]); err != nil {
        return nil, fmt.Errorf("raw url invalid: %s", slices[1])
    }
    if host, port, err = parseHost(rawUrl.Host); err != nil {
        return nil, fmt.Errorf("host invalid: %s", err)
    }

    req := request{
        method:   slices[0],
        scheme:   rawUrl.Scheme,
        host:     host,
        port:     port,
        path:     rawUrl.Path,
        query:    rawUrl.RawQuery,
        protocol: slices[2],
    }
    return &req, nil
}

func parseHost(line string) (host string, port int, err error) {
    hostSegments := strings.Split(line, ":")
    if len(hostSegments) == 1 {
        host = hostSegments[0]
        port = 80
    } else if len(hostSegments) == 2 {
        host = hostSegments[0]
        if port, err = strconv.Atoi(hostSegments[1]); err != nil {
            return host, port, fmt.Errorf("host port must integer: %s", hostSegments[1])
        }
    } else {
        return host, port, fmt.Errorf("raw url host invalid: %s", line)
    }
    return
}

func readHeaders(reader *bufio.Reader) ([]*header, error) {
    var (
        headers         []*header
        omitHeaderNames = map[string]interface{}{"Connection": nil}
    )
    for {
        if line, err := readLine(reader); err != nil {
            return headers, err
        } else {
            if len(line) == 0 {
                return headers, nil
            }
            if header, err := parseHeader(line); err != nil {
                return headers, err
            } else {
                if _, ok := omitHeaderNames[header.name]; ok {
                    continue
                }
                logger.Debugw("receive header: ", "name", header.name, "value", header.value)
                headers = append(headers, header)
            }
        }
    }
}

func parseHeader(line string) (*header, error) {
    segs := strings.Split(line, ": ")
    if len(segs) != 2 {
        return nil, fmt.Errorf("header line must be [name: value] format, but: %s", line)
    }
    return &header{
        name:  segs[0],
        value: segs[1],
    }, nil
}
