# lilp2p
this is a simple p2p base on devp2p
# usage
## Quick start
1. cd lilp2p/main
2. open a terminal and input the following parameters to launch one or serval bootstrap node:
  go run KadShow.go -l 30303 -i 1 -s true
  go run KadShow.go -l 30304 -i 2 -s true
  ...
3. open some new terminals and input:
  go run KadShow.go -l 30310
  go run KadShow.go -l 30311
  go run KadShow.go -l 30312
  ...
4. observe
