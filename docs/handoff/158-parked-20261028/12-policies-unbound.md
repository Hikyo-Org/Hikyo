# Frozen 18-policy design, unbound

These are the original reviewed unbound templates, not an installed or currently approved final policy set. Do not replace placeholders without the exact closure/binding checks and current applicable owner decisions. All old windows are unusable. No value-read permission is added to the writer or setup.

## hikyo-158-ambient.policy.json

Raw SHA256 `f3560aea46f7356c7aa99b8940a9da7a7e3aeee0bb60020ae4c2bf420b31b569`; canonical SHA256 `be9b30bd9a6f3e4d9ee9dd4f7f480eaaec42f8da1f104025beb8fcaa2cad38e3`.

```json
{
  "Statement": [
    {
      "Action": [
        "secretsmanager:CreateSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "secretsmanager:KmsKeyArn": "UNBOUND_NEW_KEY_ARN"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/ambient/*",
      "Sid": "CreateInWriterNamespace"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret",
        "secretsmanager:PutSecretValue",
        "secretsmanager:UpdateSecretVersionStage",
        "secretsmanager:TagResource"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/ambient/*",
      "Sid": "WriteAndInspectOwnFixture"
    },
    {
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "NumericEquals": {
          "secretsmanager:RecoveryWindowInDays": 30
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/ambient/*",
      "Sid": "ScheduleThirtyDayRecovery"
    },
    {
      "Action": [
        "kms:GenerateDataKey",
        "kms:Decrypt"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/ambient/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN",
      "Sid": "KeyUseThroughSecretsManager"
    },
    {
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:BatchGetSecretValue",
        "secretsmanager:GetRandomPassword"
      ],
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverReadValues"
    },
    {
      "Action": "secretsmanager:DeleteSecret",
      "Condition": {
        "Bool": {
          "secretsmanager:ForceDeleteWithoutRecovery": "true"
        }
      },
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverForceDelete"
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-ambient.trust.json

Raw SHA256 `7ac41ab8c9f95f56903e10d8cf8adf2a376f7c7b8cd9c303b4e21485375c4af6`; canonical SHA256 `b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188`.

```json
{
  "Statement": [
    {
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox"
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com"
      }
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-assume.policy.json

Raw SHA256 `a59e5b2bd5fe041e28bb9ef8a287b6fb5a286177c4ad0798d5ea400cde482f96`; canonical SHA256 `5e72eba03eb62d57fb0ac5d50d68e633b063b5f68dc821d7841ce77a5bc0d1ba`.

```json
{
  "Statement": [
    {
      "Action": [
        "secretsmanager:CreateSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "secretsmanager:KmsKeyArn": "UNBOUND_NEW_KEY_ARN"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/assume-role/*",
      "Sid": "CreateInWriterNamespace"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret",
        "secretsmanager:PutSecretValue",
        "secretsmanager:UpdateSecretVersionStage",
        "secretsmanager:TagResource"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/assume-role/*",
      "Sid": "WriteAndInspectOwnFixture"
    },
    {
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "NumericEquals": {
          "secretsmanager:RecoveryWindowInDays": 30
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/assume-role/*",
      "Sid": "ScheduleThirtyDayRecovery"
    },
    {
      "Action": [
        "kms:GenerateDataKey",
        "kms:Decrypt"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/assume-role/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN",
      "Sid": "KeyUseThroughSecretsManager"
    },
    {
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:BatchGetSecretValue",
        "secretsmanager:GetRandomPassword"
      ],
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverReadValues"
    },
    {
      "Action": "secretsmanager:DeleteSecret",
      "Condition": {
        "Bool": {
          "secretsmanager:ForceDeleteWithoutRecovery": "true"
        }
      },
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverForceDelete"
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-assume.trust.json

Raw SHA256 `4bc58f8ee0c1d0b17f6f9cb3b83a19396a0f4b19bb571fd9891502c3294dce04`; canonical SHA256 `c1a5c0a03a3354bab2792cdc4b1633235bba1ce99309a38f190dcba624f82a4c`.

```json
{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "sts:ExternalId": "UNBOUND_APPROVED_EXTERNAL_ID"
        }
      },
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::099256073061:role/hikyo-158-source"
      }
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-cleanup.policy.json

Raw SHA256 `24ef644f1f90b63bafb62895efa52dbfebd93d271c2a0dbb4aeb6621b4b4e374`; canonical SHA256 `7242df4500caa402bab0922e1eabae5441eb91127a677dbd07f8d889f45e88bc`.

```json
{
  "Statement": [
    {
      "Action": [
        "secretsmanager:CreateSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "secretsmanager:KmsKeyArn": "UNBOUND_NEW_KEY_ARN"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*",
      "Sid": "SeedSyntheticAdoption"
    },
    {
      "Action": [
        "secretsmanager:PutSecretValue"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*",
      "Sid": "ExternalSyntheticEdit"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*",
      "Sid": "VerifyRecordedResource"
    },
    {
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Condition": {
        "Bool": {
          "secretsmanager:ForceDeleteWithoutRecovery": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*",
      "Sid": "ForceDeleteFixture"
    },
    {
      "Action": [
        "kms:GenerateDataKey",
        "kms:Decrypt"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN",
      "Sid": "KeyUseThroughSecretsManager"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-cleanup"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj",
      "Sid": "HistoricalExactDescribe"
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-cleanup.trust.json

Raw SHA256 `7ac41ab8c9f95f56903e10d8cf8adf2a376f7c7b8cd9c303b4e21485375c4af6`; canonical SHA256 `b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188`.

```json
{
  "Statement": [
    {
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox"
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com"
      }
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-kms-denied.policy.json

Raw SHA256 `64dcbebba5730a15ad3226ff3c95ec591449526d441626016ab22a7c7ab1dc66`; canonical SHA256 `743ccfe2d9754404944cddc4510ea57be5eca6bd0294da0abaaaf68ce4f47fbe`.

```json
{
  "Statement": [
    {
      "Action": [
        "secretsmanager:CreateSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "secretsmanager:KmsKeyArn": "UNBOUND_NEW_KEY_ARN"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/kms-denied/*",
      "Sid": "CreateInWriterNamespace"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret",
        "secretsmanager:PutSecretValue",
        "secretsmanager:UpdateSecretVersionStage",
        "secretsmanager:TagResource"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/kms-denied/*",
      "Sid": "WriteAndInspectOwnFixture"
    },
    {
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "NumericEquals": {
          "secretsmanager:RecoveryWindowInDays": 30
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/kms-denied/*",
      "Sid": "ScheduleThirtyDayRecovery"
    },
    {
      "Action": [
        "kms:GenerateDataKey",
        "kms:Decrypt"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/kms-denied/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN",
      "Sid": "KeyUseThroughSecretsManager"
    },
    {
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:BatchGetSecretValue",
        "secretsmanager:GetRandomPassword"
      ],
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverReadValues"
    },
    {
      "Action": "secretsmanager:DeleteSecret",
      "Condition": {
        "Bool": {
          "secretsmanager:ForceDeleteWithoutRecovery": "true"
        }
      },
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverForceDelete"
    },
    {
      "Action": [
        "kms:GenerateDataKey",
        "kms:Decrypt"
      ],
      "Effect": "Deny",
      "Resource": "UNBOUND_NEW_KEY_ARN",
      "Sid": "RealKMSNegative"
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-kms-denied.trust.json

Raw SHA256 `7ac41ab8c9f95f56903e10d8cf8adf2a376f7c7b8cd9c303b4e21485375c4af6`; canonical SHA256 `b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188`.

```json
{
  "Statement": [
    {
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox"
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com"
      }
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-oracle.policy.json

Raw SHA256 `e71f11ad6b36bd728b9ee5a3e06b5902454b3d2a7fb3da0d3f2c3268c0f2de3e`; canonical SHA256 `2a78a1240f44df01fcc9824552745ff656e10bc90a8088649e069681980be537`.

```json
{
  "Statement": [
    {
      "Action": [
        "secretsmanager:GetSecretValue"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*",
      "Sid": "ReadOnlySyntheticFixture"
    },
    {
      "Action": [
        "kms:Decrypt"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN",
      "Sid": "KeyUseThroughSecretsManager"
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-oracle.trust.json

Raw SHA256 `7ac41ab8c9f95f56903e10d8cf8adf2a376f7c7b8cd9c303b4e21485375c4af6`; canonical SHA256 `b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188`.

```json
{
  "Statement": [
    {
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox"
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com"
      }
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-source.policy.json

Raw SHA256 `0ef4bc79be155fb824c85542de4df8ad70aaa77490e0db419befe7248db35df4`; canonical SHA256 `0b7bf375f8d88c1af3536a5800d67f2ccd7272ffbdbd139dbca82fe1f009589d`.

```json
{
  "Statement": [
    {
      "Action": [
        "sts:AssumeRole"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "sts:ExternalId": "UNBOUND_APPROVED_EXTERNAL_ID"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:iam::099256073061:role/hikyo-158-assume",
      "Sid": "AssumeOnlyFixtureWriter"
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-source.trust.json

Raw SHA256 `7ac41ab8c9f95f56903e10d8cf8adf2a376f7c7b8cd9c303b4e21485375c4af6`; canonical SHA256 `b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188`.

```json
{
  "Statement": [
    {
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox"
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com"
      }
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-static.policy.json

Raw SHA256 `8d2397675b38929104fa85597975de2a9bc8a199edbcff685d6347f2d054b979`; canonical SHA256 `3aaf3c7976d5610c2769f336c79ed69aff03aeaa82b5d56e7fe52891ef924280`.

```json
{
  "Statement": [
    {
      "Action": [
        "secretsmanager:CreateSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "secretsmanager:KmsKeyArn": "UNBOUND_NEW_KEY_ARN"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/static/*",
      "Sid": "CreateInWriterNamespace"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret",
        "secretsmanager:PutSecretValue",
        "secretsmanager:UpdateSecretVersionStage",
        "secretsmanager:TagResource"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/static/*",
      "Sid": "WriteAndInspectOwnFixture"
    },
    {
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "NumericEquals": {
          "secretsmanager:RecoveryWindowInDays": 30
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/static/*",
      "Sid": "ScheduleThirtyDayRecovery"
    },
    {
      "Action": [
        "kms:GenerateDataKey",
        "kms:Decrypt"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/static/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN",
      "Sid": "KeyUseThroughSecretsManager"
    },
    {
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:BatchGetSecretValue",
        "secretsmanager:GetRandomPassword"
      ],
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverReadValues"
    },
    {
      "Action": "secretsmanager:DeleteSecret",
      "Condition": {
        "Bool": {
          "secretsmanager:ForceDeleteWithoutRecovery": "true"
        }
      },
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverForceDelete"
    },
    {
      "Action": [
        "secretsmanager:RestoreSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/static/*",
      "Sid": "RestoreLifecycleFixture"
    },
    {
      "Action": [
        "secretsmanager:ListSecrets"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "*",
      "Sid": "ListRegionalMetadata"
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-static.trust.json

Raw SHA256 `7ac41ab8c9f95f56903e10d8cf8adf2a376f7c7b8cd9c303b4e21485375c4af6`; canonical SHA256 `b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188`.

```json
{
  "Statement": [
    {
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox"
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com"
      }
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-web.policy.json

Raw SHA256 `f7ae5641001ffea407e7127ef78f909cddd6e35851f7089362796d491e3c299c`; canonical SHA256 `b89444e780ec2ddc9c732929da61e9ebc08ba22aab036b47383025baf978419a`.

```json
{
  "Statement": [
    {
      "Action": [
        "secretsmanager:CreateSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "secretsmanager:KmsKeyArn": "UNBOUND_NEW_KEY_ARN"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/web-identity/*",
      "Sid": "CreateInWriterNamespace"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret",
        "secretsmanager:PutSecretValue",
        "secretsmanager:UpdateSecretVersionStage",
        "secretsmanager:TagResource"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/web-identity/*",
      "Sid": "WriteAndInspectOwnFixture"
    },
    {
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "NumericEquals": {
          "secretsmanager:RecoveryWindowInDays": 30
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/web-identity/*",
      "Sid": "ScheduleThirtyDayRecovery"
    },
    {
      "Action": [
        "kms:GenerateDataKey",
        "kms:Decrypt"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/web-identity/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN",
      "Sid": "KeyUseThroughSecretsManager"
    },
    {
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:BatchGetSecretValue",
        "secretsmanager:GetRandomPassword"
      ],
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverReadValues"
    },
    {
      "Action": "secretsmanager:DeleteSecret",
      "Condition": {
        "Bool": {
          "secretsmanager:ForceDeleteWithoutRecovery": "true"
        }
      },
      "Effect": "Deny",
      "Resource": "*",
      "Sid": "NeverForceDelete"
    }
  ],
  "Version": "2012-10-17"
}
```

## hikyo-158-web.trust.json

Raw SHA256 `7ac41ab8c9f95f56903e10d8cf8adf2a376f7c7b8cd9c303b4e21485375c4af6`; canonical SHA256 `b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188`.

```json
{
  "Statement": [
    {
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
          "token.actions.githubusercontent.com:sub": "repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox"
        }
      },
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com"
      }
    }
  ],
  "Version": "2012-10-17"
}
```

## setup-role-boundary.json

Raw SHA256 `acdc1abfe40ec7edddfaa87beb2e4b80b979f558bfc823f29f21343c07e3b8ee`; canonical SHA256 `51290dc8c0f03a10df8d799b77a7233a64932d7911f19568064ef86d07415d00`.

```json
{
  "Statement": [
    {
      "Action": [
        "secretsmanager:CreateSecret"
      ],
      "Condition": {
        "ArnNotEquals": {
          "aws:PrincipalArn": [
            "arn:aws:iam::099256073061:role/hikyo-158-source",
            "arn:aws:iam::099256073061:role/hikyo-158-oracle"
          ]
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "secretsmanager:KmsKeyArn": "UNBOUND_NEW_KEY_ARN"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
    },
    {
      "Action": [
        "secretsmanager:PutSecretValue",
        "secretsmanager:UpdateSecretVersionStage",
        "secretsmanager:TagResource"
      ],
      "Condition": {
        "ArnNotEquals": {
          "aws:PrincipalArn": [
            "arn:aws:iam::099256073061:role/hikyo-158-source",
            "arn:aws:iam::099256073061:role/hikyo-158-oracle"
          ]
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret"
      ],
      "Condition": {
        "ArnNotEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-source"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
    },
    {
      "Action": [
        "secretsmanager:GetSecretValue"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-oracle"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
    },
    {
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Condition": {
        "ArnNotEquals": {
          "aws:PrincipalArn": [
            "arn:aws:iam::099256073061:role/hikyo-158-source",
            "arn:aws:iam::099256073061:role/hikyo-158-oracle",
            "arn:aws:iam::099256073061:role/hikyo-158-cleanup"
          ]
        },
        "BoolIfExists": {
          "secretsmanager:ForceDeleteWithoutRecovery": "false"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "NumericEquals": {
          "secretsmanager:RecoveryWindowInDays": 30
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
    },
    {
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-cleanup"
        },
        "Bool": {
          "secretsmanager:ForceDeleteWithoutRecovery": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": [
        "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
      ]
    },
    {
      "Action": [
        "secretsmanager:RestoreSecret"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-static"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
    },
    {
      "Action": [
        "secretsmanager:ListSecrets"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-static"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "*"
    },
    {
      "Action": [
        "kms:GenerateDataKey",
        "kms:Decrypt"
      ],
      "Condition": {
        "ArnNotEquals": {
          "aws:PrincipalArn": [
            "arn:aws:iam::099256073061:role/hikyo-158-source",
            "arn:aws:iam::099256073061:role/hikyo-158-oracle",
            "arn:aws:iam::099256073061:role/hikyo-158-kms-denied"
          ]
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN"
    },
    {
      "Action": [
        "kms:Decrypt"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-oracle"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "kms:CallerAccount": "099256073061",
          "kms:ViaService": "secretsmanager.eu-central-1.amazonaws.com"
        },
        "StringLike": {
          "kms:EncryptionContext:SecretARN": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN"
    },
    {
      "Action": [
        "sts:AssumeRole"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-source"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_AUTH_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1",
          "sts:ExternalId": "UNBOUND_APPROVED_EXTERNAL_ID"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:iam::099256073061:role/hikyo-158-assume"
    },
    {
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:BatchGetSecretValue",
        "secretsmanager:GetRandomPassword"
      ],
      "Condition": {
        "ArnNotEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-oracle"
        }
      },
      "Effect": "Deny",
      "Resource": "*"
    },
    {
      "Action": "*",
      "Condition": {
        "DateGreaterThanEquals": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        }
      },
      "Effect": "Deny",
      "Resource": "*"
    },
    {
      "Action": [
        "secretsmanager:DescribeSecret"
      ],
      "Condition": {
        "ArnEquals": {
          "aws:PrincipalArn": "arn:aws:iam::099256073061:role/hikyo-158-cleanup"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj"
    }
  ],
  "Version": "2012-10-17"
}
```

## setup-user.managed-policy.json

Raw SHA256 `8bb50c3a0ea926dd41d1bc6a951ef2f1837924bea9b10894a8e2bebf40355467`; canonical SHA256 `0ed3d2d602205f96b54487a7bf8001cb1760797316f037ea60d14cfb511a94ca`.

```json
{
  "Statement": [
    {
      "Action": [
        "iam:GetUser",
        "iam:ListMFADevices",
        "iam:ChangePassword",
        "iam:GetLoginProfile"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_SETUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:iam::099256073061:user/hikyo-158-setup"
    },
    {
      "Action": [
        "iam:GetAccountPasswordPolicy"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_SETUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": "*"
    },
    {
      "Action": [
        "iam:CreateVirtualMFADevice",
        "iam:DeleteVirtualMFADevice"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_SETUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:iam::099256073061:mfa/hikyo-158-setup"
    },
    {
      "Action": [
        "iam:EnableMFADevice",
        "iam:ResyncMFADevice"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_SETUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:iam::099256073061:user/hikyo-158-setup"
    },
    {
      "Action": [
        "iam:ListVirtualMFADevices"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_SETUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": "*"
    },
    {
      "Action": [
        "signin:AuthorizeOAuth2Access",
        "signin:CreateOAuth2Token"
      ],
      "Condition": {
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_SETUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:signin:eu-central-1:099256073061:oauth2/public-client/localhost"
    },
    {
      "Action": [
        "iam:CreateRole"
      ],
      "Condition": {
        "ArnEquals": {
          "iam:PermissionsBoundary": "arn:aws:iam::099256073061:policy/hikyo-158-role-boundary"
        },
        "Bool": {
          "aws:MultiFactorAuthPresent": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": [
        "arn:aws:iam::099256073061:role/hikyo-158-static",
        "arn:aws:iam::099256073061:role/hikyo-158-ambient",
        "arn:aws:iam::099256073061:role/hikyo-158-assume",
        "arn:aws:iam::099256073061:role/hikyo-158-web",
        "arn:aws:iam::099256073061:role/hikyo-158-kms-denied",
        "arn:aws:iam::099256073061:role/hikyo-158-source",
        "arn:aws:iam::099256073061:role/hikyo-158-oracle",
        "arn:aws:iam::099256073061:role/hikyo-158-cleanup"
      ]
    },
    {
      "Action": [
        "iam:GetRole",
        "iam:DeleteRole",
        "iam:UpdateRole",
        "iam:UpdateAssumeRolePolicy",
        "iam:PutRolePolicy",
        "iam:GetRolePolicy",
        "iam:DeleteRolePolicy",
        "iam:ListRolePolicies",
        "iam:ListAttachedRolePolicies",
        "iam:TagRole",
        "iam:ListRoleTags"
      ],
      "Condition": {
        "Bool": {
          "aws:MultiFactorAuthPresent": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": [
        "arn:aws:iam::099256073061:role/hikyo-158-static",
        "arn:aws:iam::099256073061:role/hikyo-158-ambient",
        "arn:aws:iam::099256073061:role/hikyo-158-assume",
        "arn:aws:iam::099256073061:role/hikyo-158-web",
        "arn:aws:iam::099256073061:role/hikyo-158-kms-denied",
        "arn:aws:iam::099256073061:role/hikyo-158-source",
        "arn:aws:iam::099256073061:role/hikyo-158-oracle",
        "arn:aws:iam::099256073061:role/hikyo-158-cleanup"
      ]
    },
    {
      "Action": [
        "iam:CreateOpenIDConnectProvider",
        "iam:GetOpenIDConnectProvider",
        "iam:DeleteOpenIDConnectProvider",
        "iam:TagOpenIDConnectProvider",
        "iam:ListOpenIDConnectProviderTags"
      ],
      "Condition": {
        "Bool": {
          "aws:MultiFactorAuthPresent": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": "arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com"
    },
    {
      "Action": [
        "iam:ListRoles",
        "iam:ListOpenIDConnectProviders"
      ],
      "Condition": {
        "Bool": {
          "aws:MultiFactorAuthPresent": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        }
      },
      "Effect": "Allow",
      "Resource": "*"
    },
    {
      "Action": [
        "kms:DescribeKey",
        "kms:GetKeyPolicy",
        "kms:GetKeyRotationStatus"
      ],
      "Condition": {
        "Bool": {
          "aws:MultiFactorAuthPresent": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN"
    },
    {
      "Action": [
        "kms:ScheduleKeyDeletion"
      ],
      "Condition": {
        "Bool": {
          "aws:MultiFactorAuthPresent": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "NumericEquals": {
          "kms:ScheduleKeyDeletionPendingWindowInDays": 7
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      },
      "Effect": "Allow",
      "Resource": "UNBOUND_NEW_KEY_ARN"
    },
    {
      "Action": [
        "sts:AssumeRole",
        "sts:AssumeRoleWithWebIdentity",
        "sts:AssumeRoleWithSAML",
        "iam:PutRolePermissionsBoundary",
        "iam:DeleteRolePermissionsBoundary",
        "iam:CreatePolicyVersion",
        "iam:SetDefaultPolicyVersion",
        "iam:DeletePolicyVersion",
        "iam:DeletePolicy",
        "kms:PutKeyPolicy",
        "kms:CreateGrant",
        "secretsmanager:PutResourcePolicy",
        "kms:CreateKey",
        "kms:TagResource",
        "kms:UntagResource",
        "kms:CreateAlias",
        "kms:UpdateAlias",
        "kms:DeleteAlias",
        "kms:Decrypt",
        "kms:Encrypt",
        "kms:GenerateDataKey",
        "kms:ReEncryptFrom",
        "kms:ReEncryptTo",
        "secretsmanager:GetSecretValue",
        "secretsmanager:BatchGetSecretValue",
        "secretsmanager:GetRandomPassword"
      ],
      "Effect": "Deny",
      "Resource": "*"
    },
    {
      "Action": "*",
      "Condition": {
        "DateGreaterThanEquals": {
          "aws:CurrentTime": "UNBOUND_APPROVED_SETUP_UNTIL"
        }
      },
      "Effect": "Deny",
      "Resource": "*"
    },
    {
      "Condition": {
        "DateGreaterThanEquals": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        }
      },
      "Effect": "Deny",
      "NotAction": [
        "iam:ChangePassword",
        "iam:CreateVirtualMFADevice",
        "iam:DeleteVirtualMFADevice",
        "iam:EnableMFADevice",
        "iam:GetAccountPasswordPolicy",
        "iam:GetLoginProfile",
        "iam:GetUser",
        "iam:ListMFADevices",
        "iam:ListVirtualMFADevices",
        "iam:ResyncMFADevice",
        "signin:AuthorizeOAuth2Access",
        "signin:CreateOAuth2Token"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "secretsmanager:DescribeSecret"
      ],
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*",
      "Condition": {
        "Bool": {
          "aws:MultiFactorAuthPresent": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      }
    },
    {
      "Effect": "Allow",
      "Action": [
        "secretsmanager:DeleteSecret"
      ],
      "Resource": "arn:aws:secretsmanager:eu-central-1:099256073061:secret:UNBOUND_APPROVED_RUN_PREFIX/*",
      "Condition": {
        "Bool": {
          "aws:MultiFactorAuthPresent": "true",
          "secretsmanager:ForceDeleteWithoutRecovery": "true"
        },
        "DateLessThan": {
          "aws:CurrentTime": "UNBOUND_APPROVED_CLEANUP_UNTIL"
        },
        "StringEquals": {
          "aws:RequestedRegion": "eu-central-1"
        }
      }
    }
  ],
  "Version": "2012-10-17"
}
```
