# Changelog

## [0.3.1](https://github.com/vertracloud/cli/compare/v0.3.0...v0.3.1) (2026-10-22)


### Features

* **upload:** send LANGUAGE from vertracloud.config ([751b561](https://github.com/vertracloud/cli/commit/751b56107a108fd10c1bd37146ab18f077498c78))

## [0.3.0](https://github.com/vertracloud/cli/compare/v0.2.2...v0.3.0) (2026-10-22)


### ⚠ BREAKING CHANGES

* app domains commands and plan names

### Features

* app domains commands and plan names ([2bbd872](https://github.com/vertracloud/cli/commit/2bbd872a00185a6958619facba3369169fceacbc))
* confirm before app stop, db stop and app env remove ([92e1158](https://github.com/vertracloud/cli/commit/92e1158b8c6855a0ba3e2a74f1fdf3ae7b0922cd))


### Bug Fixes

* declining a domain removal exits non-zero and the prompt names the domain and app ([dfab0e7](https://github.com/vertracloud/cli/commit/dfab0e7fb367003c30427889f62bbff480c145d4))
* declining any destructive confirmation exits 1 with CANCELLED instead of 0 ([adf6ee3](https://github.com/vertracloud/cli/commit/adf6ee3e1ed230e74e60e550fe055f0149dacbfa))
* **deps:** require sdk-api-go v0.3.0 ([d53e638](https://github.com/vertracloud/cli/commit/d53e6386328c80d94f51b30bfa528b462449ab63))
* the domain removal prompt mentions the companion only when it is removed too ([0f231a8](https://github.com/vertracloud/cli/commit/0f231a8b3e72f965ead395db158ccf4094a2016b))

## [0.2.2](https://github.com/vertracloud/cli/compare/v0.2.1...v0.2.2) (2026-10-18)


### Bug Fixes

* **apps:** show building status in app list ([c6c0a4e](https://github.com/vertracloud/cli/commit/c6c0a4ee5ca995814c3f34dee8a7f36d6563bc8f))
* **apps:** wait for the async build on upload ([59e505d](https://github.com/vertracloud/cli/commit/59e505db302dc2a9c6f2ee349762d384600f6c0a))

## [0.2.1](https://github.com/vertracloud/cli/compare/v0.2.0...v0.2.1) (2026-10-15)


### Features

* **blob:** add --rename to blob put ([733445d](https://github.com/vertracloud/cli/commit/733445d9de216e7ea4af6d02c50ec1e7fd54032a))

## [0.2.0](https://github.com/vertracloud/cli/compare/v0.1.4...v0.2.0) (2026-10-15)


### Features

* add vertra blob commands ([75e0cda](https://github.com/vertracloud/cli/commit/75e0cda9032181ce772b332237cad5755097a980))


### Bug Fixes

* **blob:** accept object UUIDs in get ([d4e589f](https://github.com/vertracloud/cli/commit/d4e589fce50ca728276e5c003223c71f491a726e))

## [0.1.4](https://github.com/vertracloud/cli/compare/v0.1.3...v0.1.4) (2026-10-12)


### Features

* **apps:** releases, rollback and deploy settings ([6181242](https://github.com/vertracloud/cli/commit/618124297629ccfab275ce7126db4de76bf054f6))
* **commit:** send a commit message with -m or the last git subject ([a0b9652](https://github.com/vertracloud/cli/commit/a0b96528100a76ebffdab73eee1fb8e64d188ac1))


### Bug Fixes

* **ui:** strip terminal control sequences from text the server sends ([7260382](https://github.com/vertracloud/cli/commit/7260382f41153286c26ec5cced74ef22875139d6))

## [0.1.3](https://github.com/vertracloud/cli/compare/v0.1.2...v0.1.3) (2026-10-10)


* release 0.1.3 ([d0d5863](https://github.com/vertracloud/cli/commit/d0d5863903e7096cf2382d6f421f3c8d1fd8e74f))


### Features

* **app:** add --path destination folder to app commit ([c235b3d](https://github.com/vertracloud/cli/commit/c235b3d81f72bf8f4dced21506a51299e735b175))

## [0.1.2](https://github.com/vertracloud/cli/compare/v0.1.1...v0.1.2) (2026-10-05)


### Bug Fixes

* **deps:** bump sdk-api-go to v0.1.3 ([551e1a6](https://github.com/vertracloud/cli/commit/551e1a6abf58bb874f09f871fc99e2db9dd6d9ba))

## [0.1.1](https://github.com/vertracloud/cli/compare/v0.1.0...v0.1.1) (2026-09-30)


### ⚠ BREAKING CHANGES

* remove --autorestart flag and auto_restart display

* release 0.1.1 ([deef81b](https://github.com/vertracloud/cli/commit/deef81b3b290fdf272db54c31daa644b613d6fd4))


### Features

* remove --autorestart flag and auto_restart display ([94a312c](https://github.com/vertracloud/cli/commit/94a312cfdc7ae699dacc8a411d954b3f4384e428))

## 0.1.0 (2026-09-23)


### Features

* add install scripts for macOS, Linux and Windows ([69dce23](https://github.com/vertracloud/cli/commit/69dce23a2df34d4dd1747ee922b082cc0c5a1894))
* add vertra command entrypoint ([b973729](https://github.com/vertracloud/cli/commit/b973729bd5a510d72cbbcb030a7bac31dcacaf9b))
* **apps:** add app commands ([dc35fa9](https://github.com/vertracloud/cli/commit/dc35fa9ec27b609f668642e7afa860d431876252))
* **config:** add config file and API key storage ([034509e](https://github.com/vertracloud/cli/commit/034509e15cdb698afd2f259a8ad4258258c81c28))
* **databases:** add database commands ([9f8f9f5](https://github.com/vertracloud/cli/commit/9f8f9f5fea4b12d2f21bfd460fd26c4279001629))
* **local:** add project detection, zip packaging and doctor checks ([ae796d8](https://github.com/vertracloud/cli/commit/ae796d8e1a4c2997304ac028bc993bec08d5c832))
* **resources:** add shared resource lookup ([2097197](https://github.com/vertracloud/cli/commit/209719775d23fe280b80db9694b3ec0039effb36))
* **ui:** add table, JSON and localized output ([7dd423a](https://github.com/vertracloud/cli/commit/7dd423ad9ebf371009266a482e9b8a4f3058dc84))
