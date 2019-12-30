# /usr/bin/env bash
# bundle: log.sh
LOG_DATE_FORMAT='+%Y/%m/%d %H:%M:%S'
loginfo() {
    echo -ne '\033[1;32m'                           1>&2
    echo -n "[`date "$LOG_DATE_FORMAT"`] INFO "     1>&2
    echo $*                                         1>&2
    echo -ne '\033[0m'                              1>&2
}
__escaped_date_sed() {
    date "$LOG_DATE_FORMAT" | sed -E 's/\//\\\//g'
}
loginfo_stream() {
    echo -ne '\033[1;32m'                           1>&2
    sed -E 's/^(.*)$/['"`__escaped_date_sed`"'] INFO \1/g' 1>&2
    echo -ne '\033[0m'                              1>&2
}
logerror() {
    echo -ne '\033[1;31m'                           1>&2
    echo -n "[`date "$LOG_DATE_FORMAT"`] ERROR "    1>&2
    echo $*                                         1>&2
    echo -ne '\033[0m'                              1>&2
}
logwarn() {
    echo -ne '\033[1;33m'                           1>&2
    echo -n "[`date "$LOG_DATE_FORMAT"`] WARN "     1>&2
    echo $*                                         1>&2
    echo -ne '\033[0m'                              1>&2
}
log_exec() {
    loginfo '[exec]' $*
    $*
}
# bundle: utils.sh
path_join() {
    local -i idx=1
    local result=
    while [ $idx -le $# ]; do
        eval "local current=\$$idx"
        local -i idx=idx+1
        if [ -z "$result" ] || [ "${current:0:1}" = "/" ] ; then
            local result=$current
            local current=
        fi
        local dir=`dirname "$result/$current"`
        local base=`basename "$result/$current"`
        if [ "$dir" = '/' ]; then
            local dir=
        fi
        local result=$dir/$base
    done
    echo $result
}
set_var() {
    eval "$1="\'"$2"\'
}
hash_for_key() {
    echo "$*" | md5sum - | cut -d ' ' -f 1
}
hash_file_for_key() {
    cat $* | md5sum - | cut -d ' ' -f 1
}
strip() {
    echo "$*" | xargs
}
os_package_manager_name() {
    if command -v yum 2>&1 >/dev/null && [ `yum --version | grep -iE 'installed:\s+(rpm|yum)' | wc -l` -gt 0 ]; then
        echo yum; return 0
    fi
    if command -v apk 2>&1 >/dev/null && apk --version | grep -iqE 'apk-tools'; then
        echo apk; return 0
    fi
    if command -v dpkg 2>&1 >/dev/null; then
        echo apt; return 0
    fi
}
full_path_of() {
    echo `cd "$1"; pwd -P `
}
hash_content_for_key() {
    local target=$1
    if [ -d "$target" ]; then
        find "$target" -type f | xargs cat | md5sum - | cut -d ' ' -f 1
    elif [ -f "$target" ]; then
        cat "$target" | md5sum - | cut -d ' ' -f 1
    elif [ -e "$target" ]; then
        logerror \"$target\" not exists.
        return 1
    else
        logerror unknown file type of \"$target\"
        return 1
    fi
}
next_long_opt() {
    local "store_to=$1"
    if [ -z "$store_to" ]; then
        logerror next_long_opt: empty store variable name.
        return 1
    fi
    shift 1
    LONGOPTCANELIMATE=1
    local -i idxarg=$LONGOPTINDARG
    LONGOPTINDARG=0
    local -i idx=$LONGOPTIND
    idx=idx+idxarg
    while [ $idx -le $# ]; do
        idx=idx+1
        eval "local ____=\${$idx}"
        if [ "${____}" = "--" ]; then
            return 1
        fi
        if [ "${____:0:2}" = "--" ]; then
            eval "$store_to=\"${____:2}\""
            break
        fi
    done
    LONGOPTIND=$idx
    [ $idx -le $# ]
}
get_long_opt_arg() {
    local -i idxarg=$LONGOPTINDARG
    local -i idx=$LONGOPTIND
    local -i incre=$idxarg+1
    idx=idx+incre
    eval echo "\${$idx}"
    LONGOPTINDARG=$incre
    [ $idx -le $# ]
}
eliminate_long_opt() {
    if [ -z "$LONGOPTCANELIMATE" ]; then
        return
    fi
    local -i idxarg=$LONGOPTINDARG
    local -i idx=$LONGOPTIND
    local -i ref=idxarg+idx+1
    echo '
local -i __sar_idx=1;
local -a __sar_args=();
while test $__sar_idx -lt '$idx' ; do
    eval "__sar_args+=(\"\${$__sar_idx}\")";
    __sar_idx=__sar_idx+1;
done;
local -i __sar_ref='$ref';
while test $__sar_ref -le $# ; do
    eval "__sar_args+=(\"\${$__sar_ref}\")";
    __sar_ref=__sar_ref+1; __sar_idx=__sar_idx+1;
done;
set -- ${__sar_args[@]};'
    idx=idx-1
    echo "LONGOPTIND=$idx;"
    echo "unset LONGOPTCANELIMATE;"
}
binary_deps() {
    while [ $# -gt 0 ]; do
        if which "$1" 2>&1 > /dev/null || (set | grep -E '^'"$1"'\s+()' 2>&1 >/dev/null ); then
            shift
            continue
        fi
        MISSING_BINARY_DEP="$1"
        return 1
    done
    return 0
}
# bundle: binary.sh
# bundle: settings/bundle.sh
BUNDLE_BINARY_DIR='/tmp/sar_runtime/bin'
# bundle: bundle.sh
SAR_LAZY_LOAD_TYPE=${SAR_LAZY_LOAD_TYPE:=local}
_sar_resolve_lazy_load_path() {
    SAR_LAZY_LOAD_ROOT="$1"
    if [ -z "$SAR_LAZY_LOAD_ROOT" ]; then
        return 1
    fi
    while [ -L "$SAR_LAZY_LOAD_ROOT" ]; do
        SAR_LAZY_LOAD_ROOT=`readlink "$SAR_LAZY_LOAD_ROOT"`
    done
    SAR_LAZY_LOAD_ROOT="$( cd "$(dirname "$SAR_LAZY_LOAD_ROOT")" && pwd -P || echo '' )"
    if [ -z "$SAR_LAZY_LOAD_ROOT" ]; then
        return 1
    fi
}
if [ "$SAR_LAZY_LOAD_TYPE"="local" ] && [ -z "$SAR_BIN_BASE" ] && ! _sar_resolve_lazy_load_path "${BASH_SOURCE[0]}" && ! _sar_resolve_lazy_load_path `which "$0"`; then
    logerror "[bundle] cannot lazy loading path not detected."
fi
# bundle: lib.sh
# bundle: docker.sh
DOCKERCLI_CONFIG=~/.docker/config.json
if ! [ -e ~/.docker ]; then
    mkdir ~/.docker
fi
is_dockercli_experimentals_enabled() {
    local state=`jq '.experimental' "$DOCKERCLI_CONFIG"`
    if [ "$state" = "enabled" ]; then
        return 0
    fi
    return 1
}
enable_dockercli_experimentals() {
    if is_dockercli_experimentals_enabled; then
        return
    fi
    if ! [ -e "$DOCKERCLI_CONFIG" ] ; then
        echo "{}" > "$DOCKERCLI_CONFIG"
    fi
    TMP=/tmp/enable_dockercli_experimentals$RANDOM$RANDOM
    
    if ! cat $DOCKERCLI_CONFIG | jq 'setpath(["experimental"];"enabled")' >> "$TMP"; then
        logerror cannot enable docker experimental functions.
        return 1
    fi
    cat "$TMP" > "$DOCKERCLI_CONFIG"
}
disable_dockercli_experimentals() {
    if ! is_dockercli_experimentals_enabled; then
        return
    fi
    if ! [ -e "$DOCKERCLI_CONFIG" ] ; then
        echo "{}" > "$DOCKERCLI_CONFIG"
    fi
    TMP=/tmp/enable_dockercli_experimentals$RANDOM$RANDOM
    if cat $DOCKERCLI_CONFIG | jq 'setpath(["experimental"];"disabled")' >> "$TMP"; then
        logerror cannot disable docker experimental functions.
        return 1
    fi
    cat "$TMP" > "$DOCKERCLI_CONFIG"
}
is_image_exists() {
    enable_dockercli_experimentals
    docker manifest inspect $1 2>&1 >/dev/null
    return $?
}
docker_installed() {
    docker version -f '{{ .Client.Version }}' 2>&1 >/dev/null
    return $?
}
# bundle: builder/common.sh
_ci_get_package_ref() {
    local host=`_ci_build_generate_registry $1`
    local path=`_ci_build_generate_package_path $2`
    local env=`_ci_build_generate_env_ref "$3"`
    local tag=`_ci_build_generate_tag "$4"`
    if [ -z "$host" ] || [ -z "$path" ]; then
        return 1
    fi
    if [ ! -z "$env" ]; then
        echo -n $host/$path/$env/sar__package:$tag
    else
        echo -n $host/$path/sar__package:$tag
    fi
}
_ci_build_generate_registry() {
    local registry=`strip "$1"`
    registry=${registry:="$SAR_CI_REGISTRY"}
    registry=${registry:="docker.io"}
    echo "$registry" | sed -E 's/^(\/)*$//g'
}
_ci_build_generate_env_ref() {
    local env=`strip "$1"`
    if [ ! -z "$env" ]; then
        if echo "$env" | grep -qE '^[[:alnum:]_]+$'; then
            eval "local path_appended=\${$env}"
        fi
        
        if [ ! -z "$path_appended" ]; then
            env="$path_appended"
        fi
    else
        if [ ! -z "${GITLAB_CI+x}" ]; then
            env="$CI_COMMIT_REF_NAME"
        fi
        if [ -z "$env" ]; then
            env=`git rev-parse --abbrev-ref HEAD`
        fi
    fi
    if [ -z "$env" ]; then
        logerror "cannot resolve environment."
        return 1
    fi
    echo -n "$env" | tr -d ' ' | tr '[:upper:]' '[:lower:]' | sed 's/^_*//g'
}
_ci_build_generate_package_path() {
    local path=`strip "$1"`
    if [ -z "$path" ] && [ ! -z "${GITLAB_CI+x}" ]; then
        path="$CI_PROJECT_PATH"
    fi
    if [ -z "$path" ]; then
        logerror "project path not given."
        return 1
    fi
    echo -n "$path" | tr -d ' ' | tr '[:upper:]' '[:lower:]' | sed 's/^_*//g'
}
_ci_build_generate_tag() {
    local tag=`strip "$1"`
    if [ -z "$tag" ] && [ ! -z "${GITLAB_CI+x}" ]; then
        tag=${CI_COMMIT_SHA:0:10}
    fi
    if [ -z "$tag" ]; then
        env=`git rev-parse HEAD `
        tag=${tag:0:10}
    fi
    tag=${tag:=latest}
    echo -n "$tag" | tr -d ' ' | tr '[:upper:]' '[:lower:]' | sed 's/^_*//g'
}
_ci_build_generate_registry_prefix() {
    local host=`_ci_build_generate_registry "$1"`
    local package_path=`_ci_build_generate_package_path "$2"`
    local env=`_ci_build_generate_env_ref "$3"`
    if [ -z "$host" ] || [ -z "$package_path" ] ||  [ -z "$env" ]; then
        return 1
    fi
    echo "$host/$package_path/$env"
}
# bundle: settings/wing.sh
WING_REPORT_TYPE_START_BUILD_PACKAGE=1
WING_REPORT_TYPE_FINISH_BUILD_PACKAGE=2
# bundle: builder/archifacts.sh
ci_package_pull_help() {
    echo '
Pull archifacts.
usage:
    ci_package_pull [options] <path>
options:
      -t <tag>                            Image tag / package tag (package mode)
      -e <environment>                    Identify docker path by environment variable.
      -r <host>                           registry.
      -p <project_path>                   project path.
      -f <full_reference>                 Full reference.
example:
    ci_package_pull -p devops/runtime -e master ./my_dir
    ci_package_pull -f registry.stuhome.com/devops/runtime/master/sar__package ./my_dir
'
}
ci_package_pull() {
    if ! docker_installed ; then
        logerror [ci_package_pull] docker not installed.
        return 1
    fi
    OPTIND=0
    local ci_build_tag=
    local ci_build_host=
    local ci_package_env_name=
    local ci_build_package_path=
    local ci_full_reference=
    while getopts 't:r:e:f:p:' opt; do
        case $opt in
            t)
                local ci_build_tag=$OPTARG
                ;;
            r)
                local ci_build_host=$OPTARG
                ;;
            e)
                local ci_build_env_name=$OPTARG
                ;;
            p)
                local ci_build_package_path=$OPTARG
                ;;
            f)
                local ci_full_reference=$OPTARG
                ;;
            h)
                ci_package_pull_help
                return
                ;;
        esac
    done
    eval "local __=\${$OPTIND}"
    local target_path="`strip \"$__\"`"
    if [ -z "$target_path" ] ; then
        ci_package_pull_help
        logerror target path not specified.
        return 1
    fi
    if ! mkdir -p "$target_path"; then
        logerror cannot create directory.
        return 1
    fi
    local target_path=`full_path_of "$target_path"`
    if [ -z "$ci_full_reference" ]; then
        if [ -z "$ci_build_tag" ]; then
            local ci_build_tag=latest
        fi
        local ci_full_reference=`_ci_get_package_ref "$ci_build_host" "$ci_build_package_path" "$ci_build_env_name" "$ci_build_tag"`
    fi
    if [ -z "$ci_full_reference" ]; then
        logerror empty image reference.
        return 1
    fi
    if ! log_exec docker pull "$ci_full_reference"; then
        logerror pull package image "$ci_full_reference" failure.
        return 1
    fi
    loginfo extract package to $target_path.
    if ! docker run --entrypoint='' -v "$target_path:/_sar_package_extract_mount" "$ci_full_reference" sh -c "cp -rv /package/data/* /_sar_package_extract_mount/"; then
        logerror extract package failure.
    fi
    loginfo package extracted to $target_path.
}
_ci_auto_package() {
    if [ ! -z "${GITLAB_CI+x}" ]; then
        _ci_gitlab_package_build $*
        return $?
    fi
    _ci_build_package $*
}
_ci_build_package() {
    OPTIND=0
    local ci_package_tag=
    local ci_package_env_name=
    local ci_registry_host=
    local ci_package_path=
    local ci_no_push=
    local force_to_build=
    while getopts 't:e:r:p:sf' opt; do
        case $opt in
            t)
                local ci_package_tag=$OPTARG
                ;;
            e)
                local ci_package_env_name=$OPTARG
                ;;
            r)
                local ci_registry_host=$OPTARG
                ;;
            p)
                local ci_package_path=$OPTARG
                ;;
            s)
                local ci_no_push=1
                ;;
            f)
                local force_to_build=1
                ;;
        esac
    done
    eval "local __=\${$OPTIND}"
    local -i optind=$OPTIND
    if [ "$__" != "--" ] && [ ! -z "$__" ]; then
        local product_path=$__
    fi
    local -i optind=optind+1
    eval "local __=\${$optind}"
    if [ "$__" = "--" ]; then
        local -i optind=optind+1
    fi
    if [ -z "$product_path" ]; then
        logerror output path not specified.
        return 1
    fi
    local -i shift_opt_cnt=optind-1
    shift $shift_opt_cnt
    local ci_package_ref=`_ci_build_generate_registry_prefix "$ci_registry_host" "$ci_package_path" "$ci_package_env_name"`
    if [ -z "$ci_package_ref" ]; then
        logerror Empty package ref.
        return 1
    fi
    local ci_package_ref=`path_join "$ci_package_ref" sar__package`
    if [ -z "$ci_package_tag" ]; then
        local ci_package_tag=`_ci_build_generate_tag "$ci_package_tag"`
    fi
    if [ -z "$ci_package_tag" ]; then
        local ci_package_tag=`hash_content_for_key "$product_path"`
        if [ -z "$ci_package_tag" ]; then
            logerror cannot generate hash by files.
            return 1
        fi
        local ci_package_tag=${ci_package_tag:0:10}
    fi
    local ci_package_env_name=`_ci_build_generate_env_ref "$ci_package_env_name"`
    loginfo build package with registry image: $ci_package_ref:$ci_package_tag
    if is_image_exists "$ci_package_ref:$ci_package_tag" && [ -z "$force_to_build" ]; then
        logwarn Skip image build: "$ci_package_ref:$ci_package_tag"
        return 0
    fi
    local dockerfile_path=/tmp/Dockerfile-PACKAGE-$RANDOM$RANDOM$RANDOM
    loginfo generate dockerfile: $dockerfile_path
    if ! log_exec _ci_build_package_generate_dockerfile "$ci_package_ref" "$ci_package_env_name" "$ci_package_tag" "$product_path" > "$dockerfile_path"; then 
        logerror generate dockerfile failure.
        return 1
    fi
    if ! log_exec docker build -t $ci_package_ref:$ci_package_tag -f "$dockerfile_path" $* .; then
        logerror build failure.
        return 2
    fi
    if [ -z "$ci_no_push" ]; then
        if ! log_exec docker push $ci_package_ref:$ci_package_tag; then
            logerror uploading "image(package)" $ci_package_ref:$ci_package_tag failure.
            return 3
        fi
    fi
    if [ "$ci_package_tag" != "latest" ]; then
        log_exec docker tag "${ci_package_ref}:$ci_package_tag" "${ci_package_ref}:latest"
        if [ -z "$ci_no_push" ]; then
            if ! log_exec docker push "${ci_package_ref}:latest"; then
                logerror uploading "image(package)" ${ci_package_tag}:latest failure.
                return 4
            fi
        fi
    fi
    return 0
}
_ci_gitlab_package_build() {
    if [ -z "${GITLAB_CI+x}" ]; then
        logerror Not a Gitlab CI environment.
        return 1
    fi
    loginfo Try to login to registry.
    if ! docker login -u gitlab-ci-token -p $CI_JOB_TOKEN $CI_REGISTRY; then
        logerror Cannot login to $CI_REGISTRY.
        return 2
    fi
    OPTIND=0
    local has_ext_opt=
    local -a opts=()
    while getopts 't:r:p:e:sf' opt; do
        case $opt in
            t)
                [ "$OPTARG" = "gitlab_ci_commit_hash" ] && continue
                ;;
            r)
                ;;
            p)
                ;;
            e)
                ;;
            s)
                opts+=("-s")
                continue
                ;;
            f)
                opts+=("-f")
                continue
                ;;
        esac
        opts+=("-$opt" "$OPTARG")
    done
    local -i optind=$OPTIND-1
    eval "local __=\${$optind}"
    if [ "$__" == "--" ]; then
        local has_ext_opt="--"
    fi
    shift $optind
    log_exec _ci_build_package ${opts[@]} $has_ext_opt $*
}
_ci_build_package_generate_dockerfile() {
    local product_ref=$1
    local product_environment=$2
    local product_tag=$3
    local product_path=$4
    echo '
FROM '$PACKAGE_BASE_IMAGE'
COPY "'$product_path'" /package/data
RUN set -xe;\
    mkdir -p /_sar_package;\
    touch /_sar_package/meta;\
    echo PKG_REF='\\\'$product_ref\\\'' > /_sar_package/meta;\
    echo PKG_ENV='\\\'$product_environment\\\'' >> /_sar_package/meta;\
    echo PKG_TAG='\\\'$product_tag\\\'' >> /_sar_package/meta;\
    echo PKG_TYPE=package >> /_sar_package/meta;\
    mkdir -p /_sar_package/data;\
    rm -rf /package/data/.git /package/data/.gitlab-ci.yml;
'
    
}
_ci_wing_report() {
    local params="$1"
    local saved=wing-report-`hash_for_key "$WING_REPORT_URL $WING_CI_TOKEN $params"`
    if ! curl -X POST -L -H "Wing-Auth-Token: $WING_CI_TOKEN" "$WING_REPORT_URL" -d "$params" > "$saved"; then
        return 1
    fi
    test `jq '.success' "$saved"` = "true"
}
_ci_wing_gitlab_package_build() {
    local product_path="$1"
    echo " _       __ _                  ______               _            ";
    echo "| |     / /(_)____   ____ _   / ____/____   ____ _ (_)____   ___ ";
    echo "| | /| / // // __ \ / __ \`/  / __/  / __ \ / __ \`// // __ \ / _ \ ";
    echo "| |/ |/ // // / / // /_/ /  / /___ / / / // /_/ // // / / //  __/";
    echo "|__/|__//_//_/ /_/ \__, /  /_____//_/ /_/ \__, //_//_/ /_/ \___/ ";
    echo "                  /____/                 /____/                  ";
    echo; echo;
    if [ -z "$WING_CI_TOKEN" ]; then
        logerror wing auth token is empty.
        return 1
    fi
    if [ -z "$WING_JOB_URL" ]; then
        logerror missing remote job.
        return 2
    fi
    if [ -z "$WING_REPORT_URL" ]; then
        logerror missing report endpoint.
        return 3
    fi
    local build_script=wing-build-`hash_for_key "$WING_JOB_URL"`.sh
    loginfo fetching build script from wing platform...
    if ! curl -L -H "Wing-Auth-Token: $WING_CI_TOKEN" "$WING_JOB_URL" > "$build_script"; then
        logerror fetch build script failure.
        return 1
    fi
    chmod a+x "$build_script"
    loginfo "save to: $build_script"
    if ! _ci_wing_report "product_token=$WING_PRODUCT_TOKEN&type=$WING_REPORT_TYPE_START_BUILD_PACKAGE&succeed=true&namespace=$CI_REGISTRY_IMAGE&environment=$CI_COMMIT_REF_NAME&commit_hash=$CI_COMMIT_SHA&tag=$CI_COMMIT_SHORT_SHA"; then
        logerror cannot talk to wing server.
        return 1
    fi
    if ! "./$build_script"; then
        logerror build failure.
        if ! _ci_wing_report "product_token=$WING_PRODUCT_TOKEN&reason=SCM.BuildProductFailure&type=$WING_REPORT_TYPE_FINISH_BUILD_PACKAGE&namespace=$CI_REGISTRY_IMAGE&environment=$CI_COMMIT_REF_NAME&commit_hash=$CI_COMMIT_SHA&tag=$CI_COMMIT_SHORT_SHA"; then
            logerror cannot talk to wing server.
        fi
        return 1
    fi
    if ! ci_build gitlab-package -e $CI_COMMIT_REF_NAME "$product_path"; then
        logerror upload product failure.
        if ! _ci_wing_report "product_token=$WING_PRODUCT_TOKEN&reason=SCM.UploadProductFailure&type=$WING_REPORT_TYPE_FINISH_BUILD_PACKAGE&namespace=$CI_REGISTRY_IMAGE&environment=$CI_COMMIT_REF_NAME&commit_hash=$CI_COMMIT_SHA&tag=$CI_COMMIT_SHORT_SHA"; then
            logerror cannot talk to wing server.
        fi
        return 1
    fi
    if ! _ci_wing_report "product_token=$WING_PRODUCT_TOKEN&type=$WING_REPORT_TYPE_FINISH_BUILD_PACKAGE&succeed=true&namespace=$CI_REGISTRY_IMAGE&environment=$CI_COMMIT_REF_NAME&commit_hash=$CI_COMMIT_SHA&tag=$CI_COMMIT_SHORT_SHA"; then
        logerror cannot talk to wing server.
        return 1
    fi
}
# bundle: builder/docker.sh
_ci_docker_build() {
    OPTIND=0
    local ci_build_docker_tag=
    local ci_build_docker_env_name=
    local ci_project_path=
    local ci_registry=
    local ci_no_push=
    local force_to_build=
    local hash_target_content=
    while getopts 't:e:p:r:sfh:' opt; do
        case $opt in
            t)
                local ci_build_docker_tag=$OPTARG
                ;;
            e)
                local ci_build_docker_env_name=$OPTARG
                ;;
            p)
                local ci_project_path=$OPTARG
                ;;
            r)
                local ci_registry=$OPTARG
                ;;
            s)
                local ci_no_push=1
                ;;
            f)
                local force_to_build=1
                ;;
            h)
                local hash_target_content="$OPTARG"
                ;;
        esac
    done
    eval "local __=\${$OPTIND}"
    local -i optind=$OPTIND
    if [ "$__" = "--" ]; then
        local -i optind=optind+1
    fi
    local -i shift_opt_cnt=optind-1
    shift $shift_opt_cnt
    local ci_build_docker_ref=`_ci_build_generate_registry_prefix "$ci_registry" "$ci_project_path" "$ci_build_docker_env_name"`
    if [ -z "$ci_build_docker_ref" ]; then
        logerror Empty image reference.
        return 1
    fi
    local ci_build_docker_ref_path=$ci_build_docker_ref
    if [ -z "$ci_build_docker_tag" ]; then
        if [ ! -z "$hash_target_content" ]; then
            local ci_build_docker_tag=`hash_content_for_key $hash_target_content`
            if [ -z "$ci_build_docker_tag" ]; then
                logerror cannot generate hash by files.
                return 1
            fi
            local ci_build_docker_tag=${ci_build_docker_tag:0:10}
        else
            local ci_build_docker_tag=`_ci_build_generate_tag`
        fi 
    fi
    local ci_build_docker_ref=$ci_build_docker_ref_path:$ci_build_docker_tag
    if is_image_exists "$ci_build_docker_ref" && [ -z "$force_to_build" ]; then
        logwarn Skip image build: $ci_build_docker_ref
        return 0
    fi
    if ! log_exec docker build -t $ci_build_docker_ref $*; then
        logerror build failure.
        return 2
    fi
    if [ -z "$ci_no_push" ]; then
        if ! log_exec docker push $ci_build_docker_ref; then
            logerror uploading image $ci_build_docker_ref failure.
            return 3
        fi
        if [ "$ci_build_docker_tag" != "latest" ]; then
            log_exec docker tag $ci_build_docker_ref $ci_build_docker_ref_path:latest
            if ! log_exec docker push $ci_build_docker_ref_path:latest; then
                logerror uploading image $ci_build_docker_ref_path:latest failure.
                return 4
            fi
        fi
    fi
}
_ci_gitlab_runner_docker_build() {
    if [ -z "${GITLAB_CI+x}" ]; then
        logerror Not a Gitlab CI environment.
        return 1
    fi
    loginfo Try to login to registry.
    if ! docker login -u gitlab-ci-token -p $CI_JOB_TOKEN $CI_REGISTRY; then
        logerror Cannot login to $CI_REGISTRY.
        return 2
    fi
    OPTIND=0
    local -a opts=()
    while getopts 't:r:e:p:sfh:' opt; do
        case $opt in
            t)
                local ci_build_docker_tag=$OPTARG
                [ "$ci_build_docker_tag" = "gitlab_ci_commit_hash" ] && continue
                ;;
            r)
                ;;
            p)
                ;;
            e)
                ;;
            s)
                opts+=("-s")
                continue
                ;;
            f)
                opts+=("-f")
                continue
                ;;
            h)
                ;;
        esac
        opts+=("-$opt" "$OPTARG")
    done
    local -i optind=$OPTIND-1
    eval "local __=\${$optind}"
    if [ "$__" == "--" ]; then
        local has_docker_ext="--"
    fi
    shift $optind
    log_exec _ci_docker_build ${opts[@]} $has_docker_ext $*
}
_ci_auto_docker_build() {
    if [ ! -z "${GITLAB_CI+x}" ]; then
        log_exec _ci_gitlab_runner_docker_build $*
        return $?
    fi
    log_exec _ci_docker_build $*
}
# bundle: settings/image.sh
PACKAGE_BASE_IMAGE='registry.stuhome.com/sunmxt/wing/alpine/master:3.7'
SAR_CI_REGISTRY='registry.stuhome.com'
SAR_RUNTIME_PKG_PREFIX='registry.stuhome.com/devops/runtime'
SAR_RUNTIME_PKG_ENV='sae_runtime_master'
SAR_RUNTIME_PKG_TAG='latest'
SAR_RUNTIME_PKG_PROJECT_PATH='sunmxt/wing'
SAR_RUNTIME_ALPINE_DEPENDENCIES=(
    'jq' 'bash' 'supervisor' 'coreutils' 'procps' 'vim' 'net-tools'
    'bind-tools' 'tzdata' 'gettext' 'py2-pip' 'curl'
)
SAR_RUNTIME_YUM_DEPENDENCIES=(
    'bash' 'supervisor' 'jq' 'vim' 'python' 'net-tools' 'tzdata'
    'gettext' 'python-pip' 'coreutils' 'procps' 'curl' 'bind-utils'
)
SAR_RUNTIME_APT_DEPENDENCIES=(
    'bash' 'supervisor' 'jq' 'vim' 'python' 'net-tools' 'tzdata'
    'gettext' 'python-pip' 'coreutils' 'procps' 'curl' 'dnsutils'
)
SAR_RUNTIME_SYS_PYTHON_DEPENDENCIES=(
    'ipython'
)
SAR_PYTHON_MIRRORS='https://pypi.tuna.tsinghua.edu.cn/simple'
SAR_RUNTIME_APP_DEFAULT_WORKING_DIR='/'
# bundle: builder/ci.sh
enable_dockercli_experimentals
help_ci_build() {
    echo '
Project builder in CI Environment.
ci_build <mode> [options] -- [docker build options]
mode:
  docker                build and push to gitlab registry.
  package               upload archifacts in local development environment.
options:
      -t <tag>                            Image tag / package tag (package mode)
                                          if `gitlab_ci_commit_hash` is specified in 
                                          `gitlab-runner-docker` mode, the tag will be substitute 
                                          with actually commit hash.
      -e <environment_variable_name>      Identify docker path by environment variable.
      -r <host>                           registry.
      -p <project_path>                   project path.
      -s                                  Do not push image to regsitry.
      -h <path_to_hash>                   Use file(s) hash for tag.
      -f                                  force to build.
example:
      ci_build package -p be/recruitment2019 bin
      ci_build docker -p be/recruitment2019 .
      ci_build docker -e dev -- --build-arg="myvar=1" .
      ci_build docker -t gitlab_ci_commit_hash -r registry.mine.cn -p test/myimage -e dev -- --build-arg="myvar=1" .
      ci_build docker -t stable_version -r registry.mine.cn/test/myimage .
'
}
ci_build() {
    local mode=$1
    shift 1
    case $mode in
        docker)
            _ci_auto_docker_build $*
            return $?
            ;;
        package)
            _ci_auto_package $*
            return $?
            ;;
        wing-gitlab)
            _ci_wing_gitlab_package_build $*
            return $?
            ;;
        *)
            logerror unsupported ci type: $mode
            help_ci_build
            return 1
            ;;
    esac
}
ci_login_help() {
    echo '
Sign up to ci environment.
usage:
    ci_login [-p password] <username>
'
}
ci_login() {
    if ! docker_installed ; then
        logerror [ci_login] docker not installed.
        return 1
    fi
    OPTIND=0
    while getopts 'p:' opt; do
        case $opt in
            p)
                local password=$OPTARG
                ;;
            *)
                logerror unsupported option: $opt.
                return 1
                ;;
        esac
    done
    local password
    if [ ! -z "$password" ]; then
        logwarn Attention! Specifing plain password is not recommanded.
    else
        while [ -z "$password" ]; do
            loginfo Empty password.
            echo -n 'Password: '
            read -s password
            echo
        done
    fi
    eval "local __=\${$OPTIND}"
    local username="`strip \"$__\"`"
    if [ -z "$username" ] ; then
        logerror username is empty.
        return 1
    fi
    docker login "$SAR_CI_REGISTRY" -u "$username" -p "$password"
}
# bundle: builder/validate.sh
_validate_base_image() {
    return 0
}
_validate_dependency_package() {
    return 0
}
# bundle: builder/runtime_image.sh
_runtime_image_stash_prefix() {
    local prefix=$1
    local env=$2
    local tag=$3
    `hash_for_key "$prefix" "$env" "$tag"`
}
_runtime_image_stash_prefix_by_context() {
    eval "local stash_prefix=\$_SAR_RT_BUILD_${context}_STASH_PREFIX"
    echo $stash_prefix
}
_generate_runtime_image_dockerfile_add_os_deps_centos() {
    logerror "[runtime_image_builder] Centos will be supported soon."
}
_generate_runtime_image_dockerfile_add_os_deps_debian() {
    logerror "[runtime_image_builder] Debian will be supported soon."
}
_generate_supervisor_system_service() {
    local name=$1
    local workdir=$2
    shift 2
    local exec="$*"
    echo '
[program:'$name']
command='$exec'
startsecs=20
autorestart=true
stdout_logfile=/dev/stdout
stderr_logfile=/dev/stderr
directory='$workdir'
'
}
_generate_supervisor_cron_service() {
    local name=$1
    local cron=$2
    local workdir=$3
    shift 3
    local exec="$*"
    echo '
'
}
_generate_supervisor_normal_service() {
    _generate_supervisor_system_service $*
    return $?
}
_generate_runtime_image_dockerfile_add_supervisor_services() {
    local context=$1
    loginfo "[runtime_image_build] add supervisor services."
    local supervisor_root_config=supervisor-$RANDOM$RANDOM$RANDOM.ini
    echo -n "RUN echo \"`echo '
[unix_http_server]
file=/run/supervisord.sock
[include]
files = /etc/supervisor.d/services/*.conf
[supervisord]
logfile=/var/log/supervisord.log
logfile_maxbytes=0           
loglevel=info                
pidfile=/run/runtime/supervisord.pid 
nodaemon=true                
[rpcinterface:supervisor]
supervisor.rpcinterface_factory = supervisor.rpcinterface:make_main_rpcinterface
[supervisorctl]
serverurl=unix:///run/supervisord.sock
' | base64 | tr -d '\n'`\" | base64 -d > /etc/sar_supervisor.conf"
    eval "local mkeddir_`hash_for_key /var/log/application`=1"
    echo -ne ';\\\n mkdir -p /var/log/application'
    eval "local mkeddir_`hash_for_key /run/runtime`=1"
    echo -ne ';\\\n mkdir -p /run/runtime'
    eval "local -a keys=\${_SAR_RT_BUILD_${context}_SVCS[@]}"
    for key in ${keys[@]}; do
        eval "local type=\${_SAR_RT_BUILD_${context}_SVC_${key}_TYPE}"
        eval "local name=\${_SAR_RT_BUILD_${context}_SVC_${key}_NAME}"
        eval "local exec=\${_SAR_RT_BUILD_${context}_SVC_${key}_EXEC}"
        eval "local working_dir=\${_SAR_RT_BUILD_${context}_SVC_${key}_WORKING_DIR}"
        if [ -z "$working_dir" ]; then
            local working_dir=$SAR_RUNTIME_APP_DEFAULT_WORKING_DIR
        fi
        eval "local mked=\${mkeddir_`hash_for_key \"$working_dir\"`}"
        if [ -z "$mked" ]; then
            eval "local mkeddir_`hash_for_key \"$working_dir\"`=1"
            echo -ne ';\\\n mkdir -p "'"$working_dir"'"'
        fi
        local file="supervisor-svc-$key.conf"
        echo -ne ';\\\n echo '\'''
        ( case $type in
            cron)
                eval "locak cron=\${_SAR_RT_BUILD_${context}_SVC_${key}_CRON}"
                if ! _generate_supervisor_cron_service "$name" "$cron" "$working_dir" $exec ; then
                    logerror "[runtime_image_builder] Generate cronjob $name configuration failure." 
                    return 1
                fi
                ;;
            system)
                if ! _generate_supervisor_system_service "$name" "$working_dir" $exec ; then
                    logerror "[runtime_image_builder] Generate system service $name configuration failure." 
                    return 1
                fi
                ;;
            normal)
                if ! _generate_supervisor_normal_service "$name" "$working_dir" $exec ; then
                    logerror "[runtime_image_builder] Generate normal service $name configuration failure." 
                    return 1
                fi
                ;;
            *)
                logerror "[runtime_image_builder] Unsupported service type."
                return 1
                ;;
        esac ) | base64 | tr -d '\n'
        echo -ne \'"| base64 -d > \"$file\""
    done
}
_generate_runtime_image_dockerfile_prebuild_scripts() {
    local context=$1
    eval "local pre_build_script_keys=\$_SAR_RT_BUILD_${context}_PRE_BUILD_SCRIPTS"
    if [ ! -z "$pre_build_script_keys" ]; then
        local pre_build_script_keys=`echo $pre_build_script_keys | xargs -n 1 echo | sort | uniq | sed -E 's/(.*)/'\\\\\''\1'\\\\\''/g' | xargs echo`
        local pre_build_work_dirs=
        for key in `echo $pre_build_script_keys`; do
            eval "local pre_build_script_path=\$_SAR_RT_BUILD_${context}_PRE_BUILD_SFRIPT_${key}_PATH"
            eval "local pre_build_script_workdir=\$_SAR_RT_BUILD_${context}_PRE_BUILD_SCRIPT_${key}_WORKDIR"
            if [ -z "$pre_build_script_workdir" ]; then
                local pre_build_script_workdir=/
            fi
            local pre_build_work_dirs="$pre_build_work_dirs '$pre_build_script_workdir'"
            local pre_build_scripts="$pre_build_scripts '$pre_build_script_path'"
        done
        local pre_build_scripts=`echo $pre_build_scripts | xargs -n 1 echo | sort | uniq | sed -E 's/(.*)/'\\\\\''\1'\\\\\''/g' | xargs echo`
        local pre_build_work_dirs=`echo $pre_build_work_dirs | xargs -n 1 echo | sort | uniq | sed -E 's/(.*)/'\\\\\''\1'\\\\\''/g' | xargs echo`
        local failure=0
        echo $pre_build_scripts | xargs -n 1 -I {} test -f {} || (local failure=1; logerror some pre-build script missing.)
        if [ ! $failure -eq 0 ]; then
            return 1
        fi
        echo "COPY $pre_build_scripts /_sar_package/pre_build_scripts/"
        echo -n '
RUN     set -xe;\
        cd /_sar_package/pre_build_scripts;\
        chmod a+x *; mkdir -p '$pre_build_work_dirs';'
        for key in `echo $pre_build_script_keys`; do
            eval "local pre_build_script_path=\$_SAR_RT_BUILD_${context}_PRE_BUILD_SFRIPT_${key}_PATH"
            eval "local pre_build_script_workdir=\$_SAR_RT_BUILD_${context}_PRE_BUILD_SCRIPT_${key}_WORKDIR"
            if [ -z "$pre_build_script_workdir" ]; then
                local pre_build_script_workdir=/
            fi
            local script_name=`eval "basename $pre_build_script_path"`
            local script_name=`strip $script_name`
            local script_name=`path_join /_sar_package/pre_build_scripts $script_name`
            echo -n "cd $pre_build_script_workdir; $script_name;"
        done
        echo
    fi
}
_generate_runtime_image_dockerfile_postbuild_scripts() {
    local context=$1
    eval "local post_build_script_keys=\$_SAR_RT_BUILD_${context}_POST_BUILD_SCRIPTS"
    if [ ! -z "$post_build_script_keys" ]; then
        local post_build_script_keys=`echo $post_build_script_keys | xargs -n 1 echo | sort | uniq | sed -E 's/(.*)/'\\\\\''\1'\\\\\''/g' | xargs echo`
        for key in `echo $post_build_script_keys`; do
            eval "local post_build_script_path=\$_SAR_RT_BUILD_${context}_POST_BUILD_SFRIPT_${key}_PATH"
            eval "local post_build_script_workdir=\$_SAR_RT_BUILD_${context}_POST_BUILD_SCRIPT_${key}_WORKDIR"
            local post_build_scripts="$post_build_scripts '$post_build_script_path'"
            if [ -z "$post_build_script_workdir" ]; then
                local post_build_script_workdir=/
            fi
            local post_build_work_dirs="$post_build_work_dirs '$post_build_script_workdir'"
        done
        local post_build_scripts=`echo $post_build_scripts | xargs -n 1 echo | sort | uniq | sed -E 's/(.*)/'\\\\\''\1'\\\\\''/g' | xargs echo`
        local post_build_work_dirs=`echo $post_build_work_dirs | xargs -n 1 echo | sort | uniq | sed -E 's/(.*)/'\\\\\''\1'\\\\\''/g' | xargs echo`
        local failure=0
        echo $post_build_scripts | xargs -n 1 -I {} test -f {} || (local failure=1; logerror some post-build script missing.)
        if [ ! $failure -eq 0 ]; then
            return 1
        fi
        echo "COPY $post_build_scripts /_sar_package/post_build_scripts/"
        echo -n '
RUN     set -xe;\
        cd /_sar_package/post_build_scripts;\
        chmod a+x *; mkdir -p '$post_build_work_dirs';'
        for key in `echo $post_build_script_keys`; do
            eval "local post_build_script_path=\$_SAR_RT_BUILD_${context}_POST_BUILD_SFRIPT_${key}_PATH"
            eval "local post_build_script_workdir=\$_SAR_RT_BUILD_${context}_POST_BUILD_SCRIPT_${key}_WORKDIR"
            if [ -z "$post_build_script_workdir" ]; then
                local post_build_script_workdir=/
            fi
            local script_name=`eval "basename $post_build_script_path"`
            local script_name=`strip $script_name`
            local script_name=`path_join /_sar_package/post_build_scripts $script_name`
            echo -n "cd $post_build_script_workdir; $script_name;"
        done
        echo
    fi
}
_generate_runtime_image_dockerfile_deps() {
    echo '
RUN set -xe; \
    command -v yum 2>&1 >/dev/null && [ `yum --version | grep -iE '\''installed:\s+(rpm|yum)'\'' | wc -l` -gt 0 ] && (\
        which bash || yum install -y bash;\
        bash -c '\'`declare -p SAR_RUNTIME_YUM_DEPENDENCIES`'; yum install -y ${SAR_RUNTIME_YUM_DEPENDENCIES[@]}'\''; \
    ) ||( command -v apk 2>&1 >/dev/null && apk --version | grep -iqE '\''apk-tools'\'' && ( \
        mkdir -p /tmp/apk-cache;\
        apk update --cache-dir /tmp/apk-cache;\
        which bash || apk add bash;\
        bash -c '\'`declare -p SAR_RUNTIME_ALPINE_DEPENDENCIES`'; apk add ${SAR_RUNTIME_ALPINE_DEPENDENCIES[@]}'\''; \
        rm -rf /tmp/apk-cache;\
    ) || (command -v dpkg 2>&1 >/dev/null && (\
        apt update;\
        which bash || apt install bash;\
        bash -c '\'`declare -p SAR_RUNTIME_APT_DEPENDENCIES`'; apt install -y ${SAR_RUNTIME_APT_DEPENDENCIES[@]}'\''; \
    ) || (echo unknown package manager.; false)))
'
    if [ ${#SAR_RUNTIME_SYS_PYTHON_DEPENDENCIES[@]} -gt 0 ]; then
        echo '
RUN set -xe; \
    bash -c '\'`declare -p SAR_RUNTIME_SYS_PYTHON_DEPENDENCIES`';pip install pip -U -i "'${SAR_PYTHON_MIRRORS}'" && pip config set global.index-url "'${SAR_PYTHON_MIRRORS}'" && pip install ${SAR_RUNTIME_SYS_PYTHON_DEPENDENCIES[@]} '\'''
    fi
}
_generate_runtime_image_dockerfile() {
    local context=$1
    local package_project_path=$2
    local package_env=$3
    local pakcage_tag=$4
    local build_id=$RANDOM$RANDOM$RANDOM$RANDOM
    eval "local -a dep_keys=\${_SAR_RT_BUILD_${context}_DEPS[@]}"
    local failure=0
    local -i idx=1
    for key in ${dep_keys[@]}; do
        eval "local pkg_env_name=\${_SAR_RT_BUILD_${context}_DEP_${key}_ENV}"
        eval "local pkg_project_path=\${_SAR_RT_BUILD_${context}_DEP_${key}_PROJECT_PATH}"
        eval "local pkg_registry=\${_SAR_RT_BUILD_${context}_DEP_${key}_REGISTRY}"
        eval "local pkg_tag=\${_SAR_RT_BUILD_${context}_DEP_${key}_TAG}"
        local pkg_image_ref=`_ci_get_package_ref "$pkg_registry" "$pkg_project_path" "$pkg_env_name" "$pkg_tag"`
        loginfo "[runtime_image_builder][pre_check] check package: $pkg_image_ref"
        if ! _validate_dependency_package "$pkg_registry" "$pkg_project_path" "$pkg_env_name" "$pkg_tag"; then
            local failure=1
        fi
    done
    if [ $failure -ne 0 ]; then
        logerror "[runtime_image_builder]" dependency package validation failure.
        return 1
    fi
    local -i idx=1
    for key in ${dep_keys[@]}; do
        eval "local pkg_env_name=\${_SAR_RT_BUILD_${context}_DEP_${key}_ENV}"
        eval "local pkg_project_path=\${_SAR_RT_BUILD_${context}_DEP_${key}_PROJECT_PATH}"
        eval "local pkg_tag=\${_SAR_RT_BUILD_${context}_DEP_${key}_TAG}"
        eval "local pkg_registry=\${_SAR_RT_BUILD_${context}_DEP_${key}_REGISTRY}"
        local pkg_image_ref=`_ci_get_package_ref "$pkg_registry" "$pkg_project_path" "$pkg_env_name" "$pkg_tag"`
        loginfo "[runtime_image_builder] package $pkg_image_ref used."
        echo "FROM $pkg_image_ref AS sar_stage_`hash_for_key $build_id $pkg_image_ref`"
    done
    eval "local base_image=\$_SAR_RT_BUILD_${context}_BASE_IMAGE"
    _validate_base_image "$base_image" || return 1
    echo "FROM $base_image"
    if ! _generate_runtime_image_dockerfile_prebuild_scripts $context; then
        return 1
    fi
    _generate_runtime_image_dockerfile_deps
    for key in ${dep_keys[@]}; do
        eval "local pkg_env_name=\${_SAR_RT_BUILD_${context}_DEP_${key}_ENV}"
        eval "local pkg_project_path=\${_SAR_RT_BUILD_${context}_DEP_${key}_PROJECT_PATH}"
        eval "local pkg_tag=\${_SAR_RT_BUILD_${context}_DEP_${key}_TAG}"
        eval "local pkg_registry=\${_SAR_RT_BUILD_${context}_DEP_${key}_REGISTRY}"
        eval "local placed_path=\${_SAR_RT_BUILD_${context}_DEP_${key}_PLACE_PATH}"
        local pkg_image_ref=`_ci_get_package_ref "$pkg_registry" "$pkg_project_path" "$pkg_env_name" "$pkg_tag"`
        loginfo "[runtime_image_builder] place package $pkg_image_ref --> $placed_path"
        echo "COPY --from=sar_stage_`hash_for_key $build_id $pkg_image_ref` /package/data \"$placed_path\""
    done
    if ! _generate_runtime_image_dockerfile_add_supervisor_services $context; then
        logerror "[runtime_image_builder] failed to add supervisor services."
        return 1
    fi
    echo '
RUN set -xe;\
    [ -d '\''/_sar_package/runtime_install'\'' ] && (echo install runtime; bash /_sar_package/runtime_install/install.sh /opt/runtime );\
    mkdir -p /_sar_package;\
    echo PKG_REF='\\\'$package_project_path\\\'' > /_sar_package/meta;\
    echo PKG_ENV='\\\'$package_env\\\'' >> /_sar_package/meta;\
    echo PKG_TAG='\\\'$pakcage_tag\\\'' >> /_sar_package/meta;\
    echo PKG_TYPE=runtime_image >> /_sar_package/meta
'
    if ! _generate_runtime_image_dockerfile_postbuild_scripts $context; then
        return 1
    fi
    echo 'ENTRYPOINT [""]'
    echo 'CMD ["supervisord", "-c", "/etc/sar_supervisor.conf"]'
}
runtime_image_init_system_dependencies() {
    local pkg_mgr=`os_package_manager_name`
    case $pkg_mgr in
        apk)
            _runtime_image_init_system_dependencies_for_apk || return 1
            ;;
        yum)
            _runtime_image_init_system_dependencies_for_yum || return 1
            ;;
        apt)
            _runtime_image_init_system_dependencies_for_apt || return 1
            ;;
        *)
            logerror "[runtime_image_builder] unsupported package manager type: $pkg_mgr"
            return 1
            ;;
    esac
    _runtime_image_init_system_dependencies_for_python
}
build_runtime_image_help() {
    echo '
Build runtime image.
usage:
    build_runtime_image [options] -- [docker build options]
options:
    -c <context_name>       specified build context. (default: system)
    -s, --no-push           do not push image to registry.
    -h <path_to_hash>       use file(s) hash for tag.
    -p                      project path            (default: "'`_ci_build_generate_package_path 2>/dev/null`'")
    -t                      tag                     (default: "'`_ci_build_generate_tag 2>/dev/null`'")
    -r                      registry                (default: "'`_ci_build_generate_registry 2>/dev/null`'")
    -e, --env               environment             (default: "'`_ci_build_generate_env_ref 2>/dev/null`'")
    -f                      force to build
    --ignore-runtime        do not install runtime scripts to image.
example:
    build_runtime_image
    build_runtime_image -t latest -e staging
    build_runtime_image -r docker.io
'
}
build_runtime_image() {
    LONGOPTIND=0
    local ignore_runtime=
    local ci_image_env_name=
    local ci_no_push=
    while next_long_opt opt $*; do
        case $opt in
            ignore-runtime)
                local ignore_runtime=1
                ;;
            env)
                local ci_image_env_name=`get_long_opt_arg`
                ;;
            no-push)
                local ci_no_push=1
                ;;
        esac
        eval `eliminate_long_opt`
    done
    OPTIND=0
    local -a opts=()
    local ci_image_tag=
    local ci_package_env_name=
    local ci_registry=
    local ci_project_path=
    local context=
    local path_to_hash=
    while getopts 't:e:r:c:p:sh:f' opt; do
        case $opt in
            t)
                local ci_image_tag=$OPTARG
                ;;
            e)
                local ci_image_env_name=`_ci_build_generate_env_ref "$OPTARG"`
                ;;
            r)
                local ci_registry=$OPTARG
                ;;
            p)
                local ci_project_path=$OPTARG
                ;;
            c)
                local context=$OPTARG
                continue
                ;;
            s)
                opts+=("-s")
                continue
                ;;
            h)
                ;;
            f)
                opts+=("-f")
                continue
                ;;
            *)
                build_runtime_image_help
                logerror "[runtime_image_builder]" unexcepted options -$opt.
                return 1
                ;;
        esac
        opts+=("-$opt" "$OPTARG")
    done
    local -i optind=$OPTIND-1
    eval "local __=\${$optind}"
    if [ "$__" == "--" ]; then
        local has_docker_ext="--"
    fi
    shift $optind
    if [ -z "$context" ]; then
        local context=system
    fi
    
    if [ -z "$ignore_runtime" ]; then
        log_exec runtime_image_add_dependency -c "$context" -p "$SAR_RUNTIME_PKG_PROJECT_PATH" -e "$SAR_RUNTIME_PKG_ENV" -t "$SAR_RUNTIME_PKG_TAG" /_sar_package/runtime_install
    fi
    local dockerfile=/tmp/Dockerfile-RuntimeImage-$RANDOM$RANDOM$RANDOM
    if ! _generate_runtime_image_dockerfile "$context" "$ci_registry" "$ci_project_path" "$ci_image_env_name" "$ci_image_tag" > "$dockerfile" ; then
        build_runtime_image_help
        logerror "[runtime_image_builder]" generate runtime image failure.
        return 1
    fi
    log_exec _ci_auto_docker_build ${opts[@]} -- -f "$dockerfile" $* .
}
runtime_image_base_image_help() {
    echo '
Set base image of runtime image.
usage:
    runtime_image_base_image [options] <image reference>
options:
    -c <context_name>       specified build context. default: system
    -p <project_path>       project path.
    -r <registry>           registry.
    -e <environment_name>   environment name.
    -t <tag>                tag.
example:
    runtime_image_base_image alpine:3.7
    runtime_image_base_image -c my_context alpine:3.7
    runtime_image_base_image -p my/project -e master
'
}
runtime_image_base_image() {
    OPTIND=0
    local context=
    local project_path=
    local registry=
    local project_environment=
    local project_tag=
    local base_image=
    while getopts 'c:p:r:e:t:' opt; do
        case $opt in
            c)
                local context=$OPTARG
                ;;
            p)
                local project_path=$OPTARG
                ;;
            r)
                local registry=`_ci_build_generate_registry "$OPTARG"`
                ;;
            e)
                local project_environment=$OPTARG
                ;;
            t)
                local project_tag=$OPTARG
                ;;
            *)
                runtime_image_base_image_help
                logerror "[runtime_image_builder]" unexcepted options -$opt.
                ;;
        esac
    done
    eval "base_image=\${$OPTIND}"
    if [ -z "$base_image" ]; then
        if [ ! -z "$project_path" ] && [ ! -z "$project_environment" ] ; then
            base_image=`_ci_build_generate_registry_prefix "$registry" "$project_path" "$project_environment"`:`_ci_build_generate_tag "$project_tag"`
        else
            runtime_image_base_image_help
            logerror "[runtime_image_builder] base image not specifed."
            return 1
        fi
    fi
    if [ -z "$context" ]; then
        local context=system
    fi
    eval "_SAR_RT_BUILD_${context}_BASE_IMAGE=\"$base_image\""
}
runtime_image_add_dependency_help() {
    echo '
Add package dependency. Packages will be placed to during building runtime image.
usage:
    runtime_image_add_dependency -h <registry> -p <project_path> -e <environment_varaible_name> -t <tag> [options] <path>
options:
    -c <context_name>       specified build context. default: system
example:
    runtime_image_add_dependency -t c3adea1d -e staging -h be/recruitment-fe /app/statics
'
}
runtime_image_add_dependency() {
    OPTIND=0
    while getopts 't:e:r:c:p:' opt; do
        case $opt in
            t)
                local ci_package_tag=$OPTARG
                ;;
            e)
                local ci_package_env_name=$OPTARG
                ;;
            r)
                local ci_registry=$OPTARG
                ;;
            p)
                local ci_project_path=$OPTARG
                ;;
            c)
                local context=$OPTARG
                ;;
            *)
                runtime_image_add_dependency_help
                logerror "[runtime_image_builder]" unexcepted options -$opt.
                ;;
        esac
    done
    if [ -z "$context" ]; then
        local context=system
    fi
    eval "local place_path=\${$OPTIND}"
    if [ -z "$place_path" ]; then
        runtime_image_add_dependency_help
        logerror "[runtime_image_builder] runtime_image_add_dependency: Target path cannot be empty."
        return 1
    fi
    local dependency_key=`hash_for_key "$ci_package_prefix" "$ci_project_path" "$ci_package_env_name" "$ci_package_tag"`
    eval "_SAR_RT_BUILD_${context}_DEP_${dependency_key}_ENV=$ci_package_env_name"
    eval "_SAR_RT_BUILD_${context}_DEP_${dependency_key}_REGISTRY=$ci_registry"
    eval "_SAR_RT_BUILD_${context}_DEP_${dependency_key}_PROJECT_PATH=$ci_project_path"
    eval "_SAR_RT_BUILD_${context}_DEP_${dependency_key}_TAG=$ci_package_tag"
    eval "_SAR_RT_BUILD_${context}_DEP_${dependency_key}_PLACE_PATH=$place_path"
    eval "_SAR_RT_BUILD_${context}_DEPS+=(\"$dependency_key\")"
}
runtime_image_add_service_help() {
    echo '
Add service to image. Services will be started automatically after conainter started.
usage:
    runtime_image_add_service [options] <type> ...
    runtime_image_add_service [options] system <service_name> <command>
    runtime_image_add_service [options] normal <service_name> <command>
    runtime_image_add_service [options] cron <service_name> <command>
options:
    -d <path>               working directory.
    -c <context_name>       specified build context. default: system
runtime_image_add_service system conf_updator /opt/runtime/bin/runtime_conf_update.sh
runtime_image_add_service cron runtime_conf_update "5 0 * * *"
runtime_image_add_service normal nginx /sbin/nginx
'
}
_runtime_image_add_service() {
    local context=$1
    local working_dir=$2
    local type=$3
    local name=$4
    local exec="$5"
    local key=`hash_for_key $name`
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_TYPE=$type"
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_NAME=$name"
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_EXEC=\"$exec\""
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_WORKING_DIR=$working_dir"
    eval "_SAR_RT_BUILD_${context}_SVCS+=(\"$key\")"
}
_runtime_image_add_service_cron() {
    local context=$1
    local working_dir=$2
    local name=$3
    local cron=$4
    local exec="$5"
    local key=`hash_for_key $name`
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_TYPE=cron"
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_NAME=$name"
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_CRON=$cron"
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_EXEC=\"$exec\""
    eval "_SAR_RT_BUILD_${context}_SVC_${key}_WORKING_DIR=$working_dir"
    eval "_SAR_RT_BUILD_${context}_SVCS+=(\"$key\")"
}
runtime_image_add_service() {
    OPTIND=0
    while getopts 'c:d:' opt; do
        case $opt in
            c)
                local context=$OPTARG
                ;;
            d)
                local working_dir=$OPTARG
                ;;
            *)
                runtime_image_add_service_help
                logerror "[runtime_image_builder]" unexcepted options -$opt.
                ;;
        esac
    done
    local -i optind=$OPTIND-1
    shift $optind
    if [ -z "$context" ]; then
        local context=system
    fi
    local type=$1
    local name="$2"
    shift 2
    local -i idx=1
    while [ $idx -le $# ]; do
        eval "local param=\$$idx"
        if echo "$param" | grep ' ' -q ; then
            local exec="$exec '$param'"
        else
            local exec="$exec $param"
        fi
        local -i idx=idx+1
    done
    case $type in
        system)
            _runtime_image_add_service $context "$working_dir" system "$name" "$exec" || return 1
            ;;
        cron)
            _runtime_image_add_service_cron $context "$working_dir" "$name" "$exec" || return 1
            ;;
        normal)
            _runtime_image_add_service $context "$working_dir" normal "$name" "$exec" || return 1
            ;;
        *)
            logerror "[runtime_image_builder] unknown service type: $type"
            ;;
    esac
}
runtime_image_pre_build_script_help() {
    echo '
Run pre-build script within building of runtime image.
usage:
    runtime_image_pre_build_script [options] <script_path>
options:
    -d <path>               working directory.
    -c <context_name>       specified build context. default: system
example:
    runtime_image_pre_build_script install_lnmp.sh
    runtime_image_pre_build_script -c my_context install_nginx.sh
'
}
runtime_image_pre_build_script() {
    OPTIND=0
    while getopts 'c:d:' opt; do
        case $opt in
            c)
                local context=$OPTARG
                ;;
            d)
                local working_dir=$OPTARG
                ;;
            *)
                runtime_image_pre_build_script_help
                logerror "[runtime_image_builder]" unexcepted options -$opt.
                ;;
        esac
    done
    if [ -z "$context" ]; then
        local context=system
    fi
    local -i optind=$OPTIND-1
    shift $optind
    local -i idx=1
    local -i failure=0
    local script_appended=
    while [ $idx -le $# ]; do
        eval "local script=\$$idx"
        if ! [ -f "$script" ]; then
            logerror "[runtime_image_builder] not a script file: $script"
            local -i failure=1
        fi
        local script_appended="$script_appended '$script'"
        local -i idx=idx+1
    done
    if [ -z `strip $script_appended` ]; then
        runtime_image_pre_build_script_help
        logerror "script missing."
        return 1
    fi
    if [ $failure -gt 0 ]; then
        return 1
    fi
    local key=`eval "hash_file_for_key $script_appended"`
    eval "_SAR_RT_BUILD_${context}_PRE_BUILD_SCRIPTS=\"\$_SAR_RT_BUILD_${context}_PRE_BUILD_SCRIPTS \$key\""
    eval "_SAR_RT_BUILD_${context}_PRE_BUILD_SFRIPT_${key}_PATH=\"$script_appended\""
    eval "_SAR_RT_BUILD_${context}_PRE_BUILD_SCRIPT_${key}_WORKDIR=$working_dir"
}
runtime_image_post_build_script_help() {
    echo '
Run post-build script within building of runtime image.
usage:
    runtime_image_post_build_script [options] <script_path>
options:
    -d <path>               working directory.
    -c <context_name>       specified build context. default: system
example:
    runtime_image_post_build_script cleaning.sh
    runtime_image_post_build_script -c my_context send_notification.sh
'
}
runtime_image_post_build_script() {
    OPTIND=0
    while getopts 'c:d:' opt; do
        case $opt in
            c)
                local context=$OPTARG
                ;;
            d)
                local working_dir=$OPTARG
                ;;
            *)
                runtime_image_post_build_script_help
                logerror "[runtime_image_builder]" unexcepted options -$opt.
                ;;
        esac
    done
    if [ -z "$context" ]; then
        local context=system
    fi
    local -i optind=$OPTIND-1
    shift $optind
    local -i idx=1
    local -i failure=0
    local script_appended=
    while [ $idx -le $# ]; do
        eval "local script=\$$idx"
        if ! [ -f "$script" ]; then
            logerror "[runtime_image_builder] not a script file: $script"
            local -i failure=1
        fi
        local script_appended="$script_appended '$script'"
        local -i idx=idx+1
    done
    if [ $failure -gt 0 ]; then
        return 1
    fi
    if [ -z `strip $script_appended` ]; then
        runtime_image_post_build_script_help
        logerror "script missing."
        return 1
    fi
    local key=`eval "hash_file_for_key $script_appended"`
    eval "_SAR_RT_BUILD_${context}_POST_BUILD_SCRIPTS=\"\$_SAR_RT_BUILD_${context}_POST_BUILD_SCRIPTS \$key\""
    eval "_SAR_RT_BUILD_${context}_POST_BUILD_SFRIPT_${key}_PATH=\"$script_appended\""
    eval "_SAR_RT_BUILD_${context}_POST_BUILD_SCRIPT_${key}_WORKDIR=$working_dir"
}
# bundle: settings/binary.sh
# bundle: runtime.sh
# [bundle] binaries.
SAR_machine=`uname -m | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]'`
SAR_arch=`uname -o | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]'`
SAR_arch_os=${SAR_arch}_${SAR_machine}
unset SAR_machine
unset SAR_arch
mkdir -p "/tmp/sar_runtime/bin"
____sar_invoke_binary_yq() {
    if [ -z "${SAR_BINREF_yq}" ]; then
        ____sar_lazy_load_binray_yq || return 1
    fi
    ${SAR_BINREF_yq} $*
}
yq() {
    ${SAR_yq} $*
}
____sar_lazy_load_binray_yq() {
    local dst="/tmp/sar_runtime/bin/yq"
    case ${SAR_arch_os} in
        darwin_x86_64)
            loginfo loading binary: yq darwin_x86_64
            if ! ____sar_lazy_load_binary_impl "yq" "darwin_x86_64" | bash; then
                logerror yq darwin_x86_64 not loaded properly.
                return 1
            fi
            ;;
        linux_x86_64)
            loginfo loading binary: yq linux_x86_64
            if ! ____sar_lazy_load_binary_impl "yq" "linux_x86_64" | bash; then
                logerror yq linux_x86_64 not loaded properly.
                return 1
            fi
            ;;
        *)
            logerror yq ${SAR_arch_os} unsupported.
            return 1
            ;;
    esac
    if ! [ -e "$dst" ] || ! chmod a+x "$dst"; then
        logerror yq ${SAR_arch_os} not loaded properly.
        return 1
    else
        SAR_BINREF_yq="/tmp/sar_runtime/bin/yq"
    fi
}
SAR_yq=____sar_invoke_binary_yq
____sar_lazy_load_binary_impl() {
    local exec=$1
    local arch_os=$2
    local ref="$SAR_LAZY_LOAD_ROOT/sar_binary_${exec}_${arch_os}.sh"
    case $SAR_LAZY_LOAD_TYPE in
        http)
            curl -L "$ref" -o -
            return $?
            ;;
        local)
            cat "$ref"
            return $?
            ;;
        *)
            logerror "unknown lazy loading type: ${SAR_LAZY_LOAD_TYPE}"
            return 1
            ;;
    esac
}
