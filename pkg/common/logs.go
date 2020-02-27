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
)

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

	if err := os.MkdirAll(LogfilePrefix, os.FileMode(0755)); err != nil {
		log.Errorf("failed to create the log directory %s: %s", LogfilePrefix, err.Error())
	}
	logFile := LogfilePrefix + driver + ".log"
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		os.Exit(1)
	}

	// rotate the log file if too large
	if fi, err := f.Stat(); err == nil && fi.Size() > 2*MBSIZE {
		if err := f.Close(); err != nil {
			log.Errorf("failed to close the log file %s: %s", f.Name(), err.Error())
		}
		timeStr := time.Now().Format("-2006-01-02-15:04:05")
		timedLogfile := LogfilePrefix + driver + timeStr + ".log"
		if err := os.Rename(logFile, timedLogfile); err != nil {
			log.Errorf("failed to rename file from %s to %s: %s", logFile, timedLogfile, err.Error())
		}
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
