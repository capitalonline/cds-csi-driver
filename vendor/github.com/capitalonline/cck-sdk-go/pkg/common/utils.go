package common

import (
	"bytes"
	"encoding/json"
	"github.com/sirupsen/logrus"
	"io"
)

func MarshalJsonToIOReader(v interface{}) (io.Reader, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	logrus.Infof("data is: %s", data)
	return bytes.NewBuffer(data), nil
}
