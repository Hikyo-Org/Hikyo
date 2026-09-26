# Trust list for fork pull requests (#813).
#
# CI validates a fork pull request only when its author is vouched here or has
# write access to the repository (collaborators need no entry). Both fork-ci
# and trusted-ci read this file from the default branch, so a pull request
# cannot vouch for itself: vouching takes a merged change to this file.
#
# vouch also passes, without consulting this file, any login ending in "[bot]"
# and any account with write access. Every fork workflow run still needs a
# maintainer's approval.
#
# Syntax (https://github.com/mitchellh/vouch):
#   github:username
#   -github:username reason for denouncement
#
# Keep entries sorted alphabetically.
github:valentinhl
