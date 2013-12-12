package matrix

import (
	"io"
)

type InputMode int

const (
	InputModeFlow InputMode = iota // output replaces input in the current file stream (e.g. coffee -> js)
	InputModeFork                  // forks input into a new file stream (e.g. js -> [js, js.gz])
)

type OutputMode int

const (
	OutputModeFlow  OutputMode = iota // single output for each input (e.g. coffee -> js)
	OutputModeUnite                   // single output for the collection of all inputs (e.g. * -> manifest.json)
)

type Handler interface {
	Handle(in io.Reader, out io.Writer, name *string, exts *[]string) (err error)
	OutputExt() string
}

type HandlerOptions struct {
	InputMode  InputMode
	OutputMode OutputMode
}

type RegisteredHandler struct {
	Handler Handler
	Options *HandlerOptions
}

// map[input ext]map[output ext]{handler, options}
var registeredHandlers = make(map[string]map[string]*RegisteredHandler)

func Register(inExt string, outExt string, handler Handler, options *HandlerOptions) {
	if registeredHandlers[inExt] == nil {
		registeredHandlers[inExt] = make(map[string]*RegisteredHandler)
	}
	registeredHandlers[inExt][outExt] = &RegisteredHandler{handler, options}
}

func FindHandlers(inExt string) map[string]*RegisteredHandler {
	if handlers, ok := registeredHandlers[inExt]; ok {
		return handlers
	}
	return registeredHandlers["*"]
}

type FDHandler interface {
	RequiredFds() int // returns the number of required file descriptors
}

var fdBucket = make(chan struct{}, 10)

func SetFDLimit(limit int) {
	fdBucket = make(chan struct{}, limit)
}

func shouldOpenFD(n int) bool {
	for {
		select {
		case fdBucket <- struct{}{}:
			n--
			if n == 0 {
				return true
			}
		default:
			return false
		}
	}
}

func waitFD() {
	fdBucket <- struct{}{}
}

func fdClosed() {
	<-fdBucket
}
