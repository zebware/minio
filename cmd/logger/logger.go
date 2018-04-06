/*
 * Minio Cloud Storage, (C) 2015, 2016, 2017, 2018 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/minio/mc/pkg/console"
)

// global colors.
var (
	colorBold   = color.New(color.Bold).SprintFunc()
	colorYellow = color.New(color.FgYellow).SprintfFunc()
	colorRed    = color.New(color.FgRed).SprintfFunc()
)

var trimStrings []string

// Level type
type Level int8

// Enumerated level types
const (
	Error Level = iota + 1
	Fatal
)

const loggerTimeFormat string = "15:04:05 MST 01/02/2006"

var matchingFuncNames = [...]string{
	"http.HandlerFunc.ServeHTTP",
	"cmd.serverMain",
	"cmd.StartGateway",
	// add more here ..
}

func (level Level) String() string {
	var lvlStr string
	switch level {
	case Error:
		lvlStr = "ERROR"
	case Fatal:
		lvlStr = "FATAL"
	}
	return lvlStr
}

type traceEntry struct {
	Message   string            `json:"message"`
	Source    []string          `json:"source"`
	Variables map[string]string `json:"variables,omitempty"`
}
type args struct {
	Bucket string `json:"bucket,omitempty"`
	Object string `json:"object,omitempty"`
}

type api struct {
	Name string `json:"name,omitempty"`
	Args args   `json:"args,omitempty"`
}

type logEntry struct {
	Level      string     `json:"level"`
	Time       string     `json:"time"`
	API        api        `json:"api,omitempty"`
	RemoteHost string     `json:"remotehost,omitempty"`
	RequestID  string     `json:"requestID,omitempty"`
	UserAgent  string     `json:"userAgent,omitempty"`
	Cause      string     `json:"cause,omitempty"`
	Trace      traceEntry `json:"error"`
}

// quiet: Hide startup messages if enabled
// jsonFlag: Display in JSON format, if enabled
var (
	quiet, jsonFlag bool
)

// EnableQuiet - turns quiet option on.
func EnableQuiet() {
	quiet = true
}

// EnableJSON - outputs logs in json format.
func EnableJSON() {
	jsonFlag = true
	quiet = true
}

// Println - wrapper to console.Println() with quiet flag.
func Println(args ...interface{}) {
	if !quiet {
		console.Println(args...)
	}
}

// Printf - wrapper to console.Printf() with quiet flag.
func Printf(format string, args ...interface{}) {
	if !quiet {
		console.Printf(format, args...)
	}
}

// Init sets the trimStrings to possible GOPATHs
// and GOROOT directories. Also append github.com/minio/minio
// This is done to clean up the filename, when stack trace is
// displayed when an error happens.
func Init(goPath string) {
	var goPathList []string
	var defaultgoPathList []string
	// Add all possible GOPATH paths into trimStrings
	// Split GOPATH depending on the OS type
	if runtime.GOOS == "windows" {
		goPathList = strings.Split(goPath, ";")
		defaultgoPathList = strings.Split(build.Default.GOPATH, ";")
	} else {
		// All other types of OSs
		goPathList = strings.Split(goPath, ":")
		defaultgoPathList = strings.Split(build.Default.GOPATH, ":")

	}

	// Add trim string "{GOROOT}/src/" into trimStrings
	trimStrings = []string{filepath.Join(runtime.GOROOT(), "src") + string(filepath.Separator)}

	// Add all possible path from GOPATH=path1:path2...:pathN
	// as "{path#}/src/" into trimStrings
	for _, goPathString := range goPathList {
		trimStrings = append(trimStrings, filepath.Join(goPathString, "src")+string(filepath.Separator))
	}

	for _, defaultgoPathString := range defaultgoPathList {
		trimStrings = append(trimStrings, filepath.Join(defaultgoPathString, "src")+string(filepath.Separator))
	}

	// Add "github.com/minio/minio" as the last to cover
	// paths like "{GOROOT}/src/github.com/minio/minio"
	// and "{GOPATH}/src/github.com/minio/minio"
	trimStrings = append(trimStrings, filepath.Join("github.com", "minio", "minio")+string(filepath.Separator))
}

func trimTrace(f string) string {
	for _, trimString := range trimStrings {
		f = strings.TrimPrefix(filepath.ToSlash(f), filepath.ToSlash(trimString))
	}
	return filepath.FromSlash(f)
}

// getTrace method - creates and returns stack trace
func getTrace(traceLevel int) []string {
	var trace []string
	pc, file, lineNumber, ok := runtime.Caller(traceLevel)

	for ok {
		// Clean up the common prefixes
		file = trimTrace(file)
		// Get the function name
		_, funcName := filepath.Split(runtime.FuncForPC(pc).Name())
		// Skip duplicate traces that start with file name, "<autogenerated>"
		// and also skip traces with function name that starts with "runtime."
		if !strings.HasPrefix(file, "<autogenerated>") &&
			!strings.HasPrefix(funcName, "runtime.") {
			// Form and append a line of stack trace into a
			// collection, 'trace', to build full stack trace
			trace = append(trace, fmt.Sprintf("%v:%v:%v()", file, lineNumber, funcName))

			// Ignore trace logs beyond the following conditions
			for _, name := range matchingFuncNames {
				if funcName == name {
					return trace
				}
			}
		}
		traceLevel++
		// Read stack trace information from PC
		pc, file, lineNumber, ok = runtime.Caller(traceLevel)
	}
	return trace
}

func logIf(level Level, err error, msg string,
	data ...interface{}) {
	if err == nil {
		return
	}
	// Get the cause for the Error
	cause := err.Error()
	// Get full stack trace
	trace := getTrace(3)
	// Get time
	timeOfError := time.Now().UTC().Format(time.RFC3339Nano)
	// Output the formatted log message at console
	var output string
	message := fmt.Sprintf(msg, data...)
	if jsonFlag {
		logJSON, err := json.Marshal(&logEntry{
			Level: level.String(),
			Time:  timeOfError,
			Cause: cause,
			Trace: traceEntry{Source: trace, Message: message},
		})
		if err != nil {
			panic("json marshal of logEntry failed: " + err.Error())
		}
		output = string(logJSON)
	} else {
		// Add a sequence number and formatting for each stack trace
		// No formatting is required for the first entry
		trace[0] = "1: " + trace[0]
		for i, element := range trace[1:] {
			trace[i+1] = fmt.Sprintf("%8v: %s", i+2, element)
		}
		errMsg := fmt.Sprintf("[%s] [%s] %s (%s)",
			timeOfError, level.String(), message, cause)

		output = fmt.Sprintf("\nTrace: %s\n%s",
			strings.Join(trace, "\n"),
			colorRed(colorBold(errMsg)))
	}
	fmt.Println(output)

	if level == Fatal {
		os.Exit(1)
	}
}

// FatalIf :
func FatalIf(err error, msg string, data ...interface{}) {
	logIf(Fatal, err, msg, data...)
}

// LogIf :
func LogIf(ctx context.Context, err error) {
	if err == nil {
		return
	}

	req := GetReqInfo(ctx)

	if req == nil {
		req = &ReqInfo{API: "SYSTEM"}
	}

	API := "SYSTEM"
	if req.API != "" {
		API = req.API
	}

	tags := make(map[string]string)
	for _, entry := range req.GetTags() {
		tags[entry.Key] = entry.Val
	}

	// Get the cause for the Error
	message := err.Error()
	// Get full stack trace
	trace := getTrace(2)
	// Output the formatted log message at console
	var output string
	if jsonFlag {
		logJSON, err := json.Marshal(&logEntry{
			Level:      Error.String(),
			RemoteHost: req.RemoteHost,
			RequestID:  req.RequestID,
			UserAgent:  req.UserAgent,
			Time:       time.Now().UTC().Format(time.RFC3339Nano),
			API:        api{Name: API, Args: args{Bucket: req.BucketName, Object: req.ObjectName}},
			Trace:      traceEntry{Message: message, Source: trace, Variables: tags},
		})
		if err != nil {
			panic(err)
		}
		output = string(logJSON)
	} else {
		// Add a sequence number and formatting for each stack trace
		// No formatting is required for the first entry
		for i, element := range trace {
			trace[i] = fmt.Sprintf("%8v: %s", i+1, element)
		}

		tagString := ""
		for key, value := range tags {
			if value != "" {
				if tagString != "" {
					tagString += ", "
				}
				tagString += key + "=" + value
			}
		}

		apiString := "API: " + API + "("
		if req.BucketName != "" {
			apiString = apiString + "bucket=" + req.BucketName
		}
		if req.ObjectName != "" {
			apiString = apiString + ", object=" + req.ObjectName
		}
		apiString += ")"
		timeString := "Time: " + time.Now().Format(loggerTimeFormat)

		var requestID string
		if req.RequestID != "" {
			requestID = "\nRequestID: " + req.RequestID
		}

		var remoteHost string
		if req.RemoteHost != "" {
			remoteHost = "\nRemoteHost: " + req.RemoteHost
		}

		var userAgent string
		if req.UserAgent != "" {
			userAgent = "\nUserAgent: " + req.UserAgent
		}

		if len(tags) > 0 {
			tagString = "\n       " + tagString
		}

		output = fmt.Sprintf("\n%s\n%s%s%s%s\nError: %s%s\n%s",
			apiString, timeString, requestID, remoteHost, userAgent,
			colorRed(colorBold(message)), tagString, strings.Join(trace, "\n"))
	}
	fmt.Println(output)
}