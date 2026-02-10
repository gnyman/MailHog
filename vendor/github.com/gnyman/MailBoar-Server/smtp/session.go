package smtp

// http://www.rfc-editor.org/rfc/rfc5321.txt

import (
	"io"
	"log"
	"strings"

	"github.com/mailhog/data"
	"github.com/mailhog/smtp"
	"github.com/mailhog/storage"
)

// Session represents a SMTP session using net.TCPConn
type Session struct {
	conn          io.ReadWriteCloser
	proto         *smtp.Protocol
	storage       storage.Storage
	messageChan   chan *data.Message
	remoteAddress string
	isTLS         bool
	line          string
}

// Accept starts a new SMTP session using io.ReadWriteCloser
func Accept(remoteAddress string, conn io.ReadWriteCloser, storage storage.Storage, messageChan chan *data.Message, hostname string) {
	defer conn.Close()

	proto := smtp.NewProtocol()
	proto.Hostname = hostname

	session := &Session{
		conn:          conn,
		proto:         proto,
		storage:       storage,
		messageChan:   messageChan,
		remoteAddress: remoteAddress,
	}
	proto.LogHandler = session.logf
	proto.MessageReceivedHandler = session.acceptMessage
	proto.ValidateSenderHandler = session.validateSender
	proto.ValidateRecipientHandler = session.validateRecipient
	proto.ValidateAuthenticationHandler = session.validateAuthentication
	proto.GetAuthenticationMechanismsHandler = func() []string { return []string{"PLAIN"} }

	session.logf("Starting session")
	session.Write(proto.Start())
	for session.Read() == true {
	}
	session.logf("Session ended")
}

func (c *Session) validateAuthentication(mechanism string, args ...string) (errorReply *smtp.Reply, ok bool) {
	return nil, true
}

func (c *Session) validateRecipient(to string) bool {
	return true
}

func (c *Session) validateSender(from string) bool {
	return true
}

func (c *Session) acceptMessage(msg *data.SMTPMessage) (id string, err error) {
	m := msg.Parse(c.proto.Hostname)
	c.logf("Storing message %s", m.ID)
	id, err = c.storage.Store(m)
	c.messageChan <- m
	return
}

func (c *Session) logf(message string, args ...interface{}) {
	message = strings.Join([]string{"[SMTP %s]", message}, " ")
	args = append([]interface{}{c.remoteAddress}, args...)
	log.Printf(message, args...)
}

// Read reads from the underlying net.TCPConn
func (c *Session) Read() bool {
	buf := make([]byte, 1024)
	n, err := c.conn.Read(buf)

	if n == 0 {
		c.logf("Connection closed by remote host\n")
		io.Closer(c.conn).Close() // not sure this is necessary?
		return false
	}

	if err != nil {
		c.logf("Error reading from socket: %s\n", err)
		return false
	}

	text := string(buf[0:n])
	logText := strings.Replace(text, "\n", "\\n", -1)
	logText = strings.Replace(logText, "\r", "\\r", -1)
	c.logf("Received %d bytes: '%s'\n", n, logText)

	c.line += text

	for strings.Contains(c.line, "\r\n") {
		line, reply := c.proto.Parse(c.line)
		c.line = line

		if reply != nil {
			c.Write(reply)
			if reply.Status == 221 {
				io.Closer(c.conn).Close()
				return false
			}
		}
	}

	return true
}

// Write writes a reply to the underlying net.TCPConn
func (c *Session) Write(reply *smtp.Reply) {
	lines := reply.Lines()
	for _, l := range lines {
		logText := strings.Replace(l, "\n", "\\n", -1)
		logText = strings.Replace(logText, "\r", "\\r", -1)
		c.logf("Sent %d bytes: '%s'", len(l), logText)
		c.conn.Write([]byte(l))
	}
}
