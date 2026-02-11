#!/bin/bash
set -e

cd ..

IMAGE=192.168.0.51:5000/redis-operator:dev

# echo "🔨 Go 바이너리 빌드"
make build

# echo "🐳 Docker 이미지 빌드"
docker build --platform linux/amd64 -t $IMAGE .

echo "📤 레지스트리에 push"
docker push $IMAGE


echo "✅ 완료"
