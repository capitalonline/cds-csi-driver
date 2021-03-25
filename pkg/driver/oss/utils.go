package oss

import (
	"errors"
	"io/ioutil"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

func (opts *OssOpts) parsOssOpts() error {
	// parse url and bucket
	if opts.URL == "" || opts.Bucket == "" {
		return errors.New("Oss Parametes error: Url or Bucket empty ")
	}
	// parse path
	if opts.Path == "" {
		log.Warnf("oss, path is empty, using default root %s", defaultOssRoot)
		opts.Path = defaultOssRoot
	}
	// remove / if path end with /;
	for opts.Path != "/" && strings.HasSuffix(opts.Path, "/") {
		opts.Path = opts.Path[0 : len(opts.Path)-1]
	}
	return nil
}

// save ak file: bucket:ak_id:ak_secret
func  (opts *OssOpts)saveOssCredential(akFile string) error {
	newContentStr := opts.AkID + ":" + opts.AkSecret + "\n"
	if err := ioutil.WriteFile(akFile, []byte(newContentStr), 0600); err != nil {
		log.Errorf("Save Credential File failed, %s, %s", newContentStr, err)
		return err
	}
	log.Debugf("saveOssCredential, save AK and AS into %s succeed!", CredentialFile)
	return nil
}

// IsFileExisting check file exist in volume driver or not
func IsFileExisting(filename string) bool {
	_, err := os.Stat(filename)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}