# build-and-load.sh
#!/bin/bash
set -e  # 에러 발생시 중단

cd ../

VERSION="v7"
IMAGE_NAME="redis-operator"
IMAGE="${IMAGE_NAME}:${VERSION}"

make build

echo "🔨 새 이미지 빌드 중..."
docker build -t $IMAGE .


echo "📦 미니쿠베에 이미지 로드 중..."
minikube image load $IMAGE

echo ""
echo "✅ 완료! 이미지 확인:"
echo ""
echo "📦 로컬 Docker 이미지:"
LOCAL_IMAGE_ID=$(docker images $IMAGE --format "{{.ID}}" 2>/dev/null || echo "")
LOCAL_BUILD_TIME=$(docker inspect $IMAGE --format='{{.Created}}' 2>/dev/null || echo "")
docker images | grep redis-operator
echo ""
echo "🎯 미니쿠베 이미지:"
MINIKUBE_IMAGE_ID=$(minikube ssh "docker images $IMAGE --format '{{.ID}}'" 2>/dev/null || echo "")
MINIKUBE_BUILD_TIME=$(minikube ssh "docker inspect $IMAGE --format='{{.Created}}'" 2>/dev/null || echo "")
minikube image ls | grep $IMAGE
echo ""
echo "🔍 이미지 비교:"
echo "  로컬 ID:          $LOCAL_IMAGE_ID"
echo "  로컬 빌드 시간:   $LOCAL_BUILD_TIME"
echo "  미니쿠베 ID:      $MINIKUBE_IMAGE_ID"
echo "  미니쿠베 빌드 시간: $MINIKUBE_BUILD_TIME"
echo ""
