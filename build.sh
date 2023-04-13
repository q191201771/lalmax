#!/usr/bin/env bash

# 检测系统安装srt需要依赖的东西
# TODO 后期考虑直接使用静态库
OS=`uname -s`
if [ ${OS} == "Darwin"  ];then
    echo "This is macos"
    export OPENSSL_ROOT_DIR=$(brew --prefix openssl)
    export OPENSSL_LIB_DIR=$(brew --prefix openssl)"/lib"
    export OPENSSL_INCLUDE_DIR=$(brew --prefix openssl)"/include"
elif [ ${OS} == "Linux"  ];then
# Ubuntu
    echo "This is Linux"
    sudo apt-get update
    sudo apt-get upgrade
    sudo apt-get install tclsh pkg-config cmake libssl-dev build-essential

# CentOS 7
# sudo yum update
# sudo yum install tcl pkgconfig openssl-devel cmake gcc gcc-c++ make automake

# CentOS 6
# sudo yum update
# sudo yum install tcl pkgconfig openssl-devel cmake gcc gcc-c++ make automake
# sudo yum install centos-release-scl-rh devtoolset-3-gcc devtoolset-3-gcc-c++
# scl enable devtoolset-3 bash
# ./configure --use-static-libstdc++ --with-compiler-prefix=/opt/rh/devtoolset-3/root/usr/bin/

else
# 其他平台参考srt库编译
# https://github.com/Haivision/srt/tree/master/docs/build 
    echo "This is other platform"
fi

echo "build libsrt"
cd thirdparty/
tar -xvzf srt-1.5.1.tar.gz
cd srt-1.5.1
./configure
make
make install
cd ../../

export LD_LIBRARY_PATH=/usr/local/lib/

echo "build lalmax"
go build -o lalmax main.go