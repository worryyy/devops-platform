pipeline {
  agent {
    kubernetes {
      yaml """
apiVersion: v1
kind: Pod
spec:
  nodeSelector:
    platform-role: control

  imagePullSecrets:
  - name: tcr-secret

  containers:
  - name: golang
    image: golang:1.23
    command:
    - cat
    tty: true

  - name: kaniko
    image: ccr.ccs.tencentyun.com/k3s-platform/kaniko-executor:debug
    command:
    - /busybox/cat
    tty: true
    volumeMounts:
    - name: kaniko-secret
      mountPath: /kaniko/.docker

  - name: git
    image: alpine/git:2.45.2
    command:
    - cat
    tty: true

  volumes:
  - name: kaniko-secret
    secret:
      secretName: tcr-kaniko-secret
      items:
      - key: .dockerconfigjson
        path: config.json
"""
    }
  }

environment {
  IMAGE_REPO = "ccr.ccs.tencentyun.com/k3s-platform/server"
  GIT_PUSH_REPO = "https://github.com/worryyy/k3s-platform.git"
  GOPROXY = "https://goproxy.cn,direct"
  GO111MODULE = "on"
}

  stages {
    stage('Prepare') {
      steps {
        script {
          env.SHORT_SHA = sh(script: 'git rev-parse --short=8 HEAD', returnStdout: true).trim()
          env.IMAGE_TAG = "git-${env.SHORT_SHA}"
          echo "IMAGE_TAG=${env.IMAGE_TAG}"
        }
      }
    }

stage('Test') {
  steps {
    container('golang') {
      sh '''
        go env -w GOPROXY=https://goproxy.cn,direct
        go env -w GO111MODULE=on
        cd apps/api
        go mod download
        go test ./...
      '''
    }
  }
}

    stage('Build and Push Image') {
      steps {
        container('kaniko') {
          sh '''
            /kaniko/executor \
              --context "${WORKSPACE}/apps/api" \
              --dockerfile "${WORKSPACE}/apps/api/Dockerfile" \
              --destination "${IMAGE_REPO}:${IMAGE_TAG}" \
              --cache=false
          '''
        }
      }
    }

    stage('Update GitOps Values') {
      steps {
        container('git') {
          withCredentials([usernamePassword(credentialsId: 'git-https', usernameVariable: 'GIT_USER', passwordVariable: 'GIT_TOKEN')]) {
            sh '''
              git config user.name "jenkins"
              git config user.email "jenkins@example.com"

              sed -i -E "s/tag: .*/tag: ${IMAGE_TAG}/" helm-values/workloads/forum-api-business.yaml

              git add helm-values/workloads/forum-api-business.yaml
              git commit -m "release: forum-api ${IMAGE_TAG}" || exit 0

              CLEAN_REPO=$(echo "${GIT_PUSH_REPO}" | sed 's#https://##')
              git push "https://${GIT_USER}:${GIT_TOKEN}@${CLEAN_REPO}" HEAD:main
            '''
          }
        }
      }
    }
  }
}
