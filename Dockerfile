FROM ubuntu:trusty
MAINTAINER yutopp

RUN locale-gen --no-purge en_US.UTF-8
ENV LC_ALL en_US.UTF-8

RUN apt-get -y update
RUN apt-get -y upgrade

RUN apt-get -y install g++
RUN apt-get -y install git make unzip wget
RUN if [ ! -e /opt/cage ]; then mkdir -p /opt/cage; fi

RUN wget https://storage.googleapis.com/golang/go1.4.1.linux-amd64.tar.gz
RUN tar xzvf go1.4.1.linux-amd64.tar.gz -C /usr/local
RUN export GOROOT=/usr/local/go
RUN export PATH=$PATH:$GOROOT/bin

ADD host.get_packages.sh /opt/cage/host.get_packages.sh
ADD host.build_sources.sh /opt/cage/host.build_sources.sh
ADD host.build.sh /opt/cage/host.build.sh

ADD Makefile.posix /opt/cage/Makefile.posix
ADD process_cloner.src /opt/cage/process_cloner.src
ADD src/yutopp /opt/cage/src/yutopp/

RUN ln -s /usr/local/go/bin/go /usr/local/bin/.

RUN cd /opt/cage && ./host.build.sh
