package oss

const (
	AuthTypeDefault  = "saveAkFile"
	defaultOssRoot   = "/"
	CredentialFile   = "/etc/s3pass"
	defaultOtherOpts = "-o dbglevel=info -o curldbg -o allow_other -o use_path_request_style " +
		"-o umask=0 -o max_write=131072 -o big_writes -o enable_noobj_cache -o use_cache=/dev/shm -o sigv2 -o del_cache"
)
