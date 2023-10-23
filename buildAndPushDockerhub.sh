export PUSH_FLAG="--push"
export BUILD_FLAG="buildx build --platform linux/amd64"
if [ "$#" -gt 0 ]
then
	PUSH_FLAG=""
	BUILD_FLAG="build"
	echo To run locally
	echo Sample request.in.json and sample.check.json in e2e-opendoor
	echo "docker run -it --entrypoint=/bin/sh opendoor/telia-oss-github-pr-resource:dev"
	echo "cd /opt/resources"
	echo "cat <request.in.json|request.check.json> | ./in .| ./out .| ./check"
fi
# docker login --username=$DOCKER_USERNAME --password=$DOCKER_PASSWORD
docker $BUILD_FLAG -t opendoor/telia-oss-github-pr-resource:dev . $PUSH_FLAG
