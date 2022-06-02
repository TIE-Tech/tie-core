package rpc

import (
	"fmt"
	"github.com/tietemp/go-logger"
	"io/ioutil"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type serverType int

const (
	serverIPC serverType = iota
	serverHTTP
	serverWS
)

func (s serverType) String() string {
	switch s {
	case serverIPC:
		return "ipc"
	case serverHTTP:
		return "http"
	case serverWS:
		return "ws"
	default:
		panic("BUG: Not expected")
	}
}

// JSONRPC is an API backend
type JSONRPC struct {
	config     *Config
	dispatcher dispatcher
}

type dispatcher interface {
	HandleWs(reqBody []byte, conn wsConn) ([]byte, error)
	Handle(reqBody []byte) ([]byte, error)
}

// JSONRPCStore defines all the methods required
// by all the JSON RPC endpoints
type JSONRPCStore interface {
	ethStore
	networkStore
	txPoolStore
	filterManagerStore
}

type Config struct {
	Store   JSONRPCStore
	Addr    *net.TCPAddr
	ChainID uint64
}

// NewJSONRPC returns the JsonRPC http server
func NewJSONRPC(config *Config) (*JSONRPC, error) {
	srv := &JSONRPC{
		config:     config,
		dispatcher: newDispatcher(config.Store, config.ChainID),
	}

	// start http server
	if err := srv.setupHTTP(); err != nil {
		return nil, err
	}

	return srv, nil
}

func (j *JSONRPC) setupHTTP() error {
	logger.Info("[RPC] http server started", "addr", j.config.Addr.String())

	lis, err := net.Listen("tcp", j.config.Addr.String())
	if err != nil {
		return err
	}

	mux := http.DefaultServeMux
	mux.HandleFunc("/", j.handle)
	mux.HandleFunc("/ws", j.handleWs)

	srv := http.Server{
		Handler: mux,
	}

	go func() {
		if err := srv.Serve(lis); err != nil {
			logger.Error("closed http connection", "err", err)
		}
	}()

	return nil
}

// wsUpgrader defines upgrade parameters for the WS connection
var wsUpgrader = websocket.Upgrader{
	// Uses the default HTTP buffer sizes for Read / Write buffers.
	// Documentation specifies that they are 4096B in size.
	// There is no need to have them be 4x in size when requests / responses
	// shouldn't exceed 1024B
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// wsWrapper is a wrapping object for the web socket connection and logger
type wsWrapper struct {
	ws        *websocket.Conn // the actual WS connection
	writeLock sync.Mutex      // writer lock
}

// WriteMessage writes out the message to the WS peer
func (w *wsWrapper) WriteMessage(
	messageType int,
	data []byte,
) error {
	w.writeLock.Lock()
	defer w.writeLock.Unlock()
	writeErr := w.ws.WriteMessage(messageType, data)

	if writeErr != nil {
		logger.Error(
			fmt.Sprintf("Unable to write WS message, %s", writeErr.Error()),
		)
	}

	return writeErr
}

// isSupportedWSType returns a status indicating if the message type is supported
func isSupportedWSType(messageType int) bool {
	return messageType == websocket.TextMessage ||
		messageType == websocket.BinaryMessage
}

func (j *JSONRPC) handleWs(w http.ResponseWriter, req *http.Request) {
	// CORS rule - Allow requests from anywhere
	wsUpgrader.CheckOrigin = func(r *http.Request) bool { return true }

	// Upgrade the connection to a WS one
	ws, err := wsUpgrader.Upgrade(w, req, nil)
	if err != nil {
		logger.Error(fmt.Sprintf("Unable to upgrade to a WS connection, %s", err.Error()))
		return
	}

	// Defer WS closure
	defer func(ws *websocket.Conn) {
		err = ws.Close()
		if err != nil {
			logger.Error(
				fmt.Sprintf("Unable to gracefully close WS connection, %s", err.Error()),
			)
		}
	}(ws)

	wrapConn := &wsWrapper{ws: ws}

	logger.Info("[RPC] Websocket connection established")
	// Run the listen loop
	for {
		// Read the incoming message
		msgType, message, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseAbnormalClosure,
			) {
				// Accepted close codes
				logger.Warn("[RPC] Closing WS connection gracefully")
			} else {
				logger.Error("Unable to read WS message", "err", err)
			}
			break
		}

		if isSupportedWSType(msgType) {
			go func() {
				resp, handleErr := j.dispatcher.HandleWs(message, wrapConn)
				if handleErr != nil {
					logger.Error(fmt.Sprintf("Unable to handle WS request, %s", handleErr.Error()))
					_ = wrapConn.WriteMessage(
						msgType,
						[]byte(fmt.Sprintf("WS Handle error: %s", handleErr.Error())),
					)
				} else {
					_ = wrapConn.WriteMessage(msgType, resp)
				}
			}()
		}
	}
}

func (j *JSONRPC) handle(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set(
		"Access-Control-Allow-Headers",
		"Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization",
	)

	if (*req).Method == "OPTIONS" {
		return
	}
	if req.Method == "GET" {
		//nolint
		w.Write([]byte("TIE JSON-RPC"))

		return
	}
	if req.Method != "POST" {
		//nolint
		w.Write([]byte("method " + req.Method + " not allowed"))
		return
	}

	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		//nolint
		w.Write([]byte(err.Error()))

		return
	}

	// log request
	logger.Debug("[RPC] handle ReadAll", "request", string(data))

	resp, err := j.dispatcher.Handle(data)
	if err != nil {
		//nolint
		w.Write([]byte(err.Error()))
	} else {
		//nolint
		w.Write(resp)
	}
	logger.Debug("[RPC] handle Handle", "response", string(resp))
}
