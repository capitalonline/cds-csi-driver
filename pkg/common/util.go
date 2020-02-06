package common

import (
	"io"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// LogfilePrefix prefix of log file
	LogfilePrefix = "/var/log/cds/"
	// MBSIZE MB size
	MBSIZE = 1024 * 1024
	// Node metadata File
	NodeMetaDataFile = "/host/etc/cds/node-meta"
)

func GetNodeId() string {
	return func(path string) string {
		panic("implement me")
	}(NodeMetaDataFile)
}

func CreatePersistentStorage(persistentStoragePath string) error {
	return os.MkdirAll(persistentStoragePath, os.FileMode(0755))
}

// rotate log file by 2M bytes
// default print log to stdout and file both.
func SetLogAttribute(logType, driver string) {
	logType = strings.ToLower(logType)
	if logType != "stdout" && logType != "host" {
		logType = "both"
	}
	if logType == "stdout" {
		return
	}

	os.MkdirAll(LogfilePrefix, os.FileMode(0755))
	logFile := LogfilePrefix + driver + ".log"
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		os.Exit(1)
	}

	// rotate the log file if too large
	if fi, err := f.Stat(); err == nil && fi.Size() > 2*MBSIZE {
		f.Close()
		timeStr := time.Now().Format("-2006-01-02-15:04:05")
		timedLogfile := LogfilePrefix + driver + timeStr + ".log"
		os.Rename(logFile, timedLogfile)
		f, err = os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			os.Exit(1)
		}
	}
	if logType == "both" {
		mw := io.MultiWriter(os.Stdout, f)
		log.SetOutput(mw)
	} else {
		log.SetOutput(f)
	}
}
