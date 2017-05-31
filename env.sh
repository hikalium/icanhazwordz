#!/bin/sh
if [[ `dirname "$0"` != "/bin" ]]; then
  echo "Execute with \". env.sh\", not bash."
  exit 1
fi

dir=`dirname $_`
dir=`readlink -f $dir`
echo GOPATH=$dir
export GOPATH=$dir
for pkg in google.golang.org/appengine/datastore golang.org/x/tools/cmd/goimports google.golang.org/appengine; do
  echo go get $pkg
  go get $pkg
done
