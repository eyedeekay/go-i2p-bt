fmt:
	find . -name '*.go' -exec gofumpt -w -s -extra {} \;