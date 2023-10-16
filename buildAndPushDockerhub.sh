# docker login --username=$DOCKER_USERNAME --password=$DOCKER_PASSWORD
docker buildx build --platform linux/amd64 -t opendoor/telia-oss-github-pr-resource:dev . --push
