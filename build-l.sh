GOPATH=`pwd` go build -o bin/cage yutopp/cage &&
GOPATH=`pwd` go build -o bin/cage.callback yutopp/cage.callback &&
make -f Makefile.posix

# sudo docker build -t torigoya/cage