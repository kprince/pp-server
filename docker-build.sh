docker buildx build --platform linux/amd64 -t nextfluxion/pp-server:$1 .
docker push nextfluxion/pp-server:$1