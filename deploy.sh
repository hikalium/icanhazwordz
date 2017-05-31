#!/bin/sh
set -xe
gcloud app deploy --project=icanhazwordz app/app.yaml app/index.yaml
