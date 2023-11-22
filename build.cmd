@echo off
set GOARCH=amd64
set CGO_ENABLED=0
set GOOS=linux
echo "building linux..."
go build -ldflags "-w -s" --trimpath -o jarm

set GOOS=windows
echo "building windows..."
go build -ldflags "-w -s" --trimpath -o jarm.exe
echo "success!"