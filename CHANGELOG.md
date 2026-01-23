# Changelog

## [1.1.0](https://github.com/meigma/blob/compare/v1.0.0...v1.1.0) (2026-01-23)


### Features

* add progress reporting for CLI progress bars ([#48](https://github.com/meigma/blob/issues/48)) ([10f2fbe](https://github.com/meigma/blob/commit/10f2fbe93dcc3b3a5f29bb5329d093ddc3439c11))
* **core:** add CopyFile method for single-file extraction with rename ([#51](https://github.com/meigma/blob/issues/51)) ([727a0e9](https://github.com/meigma/blob/commit/727a0e97fbb37e29061779959363d82615d6e0a9))
* **core:** add DirStats method for directory statistics ([#54](https://github.com/meigma/blob/issues/54)) ([e9ed59a](https://github.com/meigma/blob/commit/e9ed59a486d71de7d51ee63cc0909f14477e0e40))
* **core:** add Exists, IsDir, IsFile convenience methods ([#53](https://github.com/meigma/blob/issues/53)) ([ffc1ac6](https://github.com/meigma/blob/commit/ffc1ac62444809955ba4937cb28b6a1ce058face))
* **core:** add NormalizePath helper for fs.ValidPath conversion ([#50](https://github.com/meigma/blob/issues/50)) ([6ddbec5](https://github.com/meigma/blob/commit/6ddbec521236d2ca134a3bf162cccd219ebd9666))
* **core:** add ValidateFiles method for bulk file validation ([#55](https://github.com/meigma/blob/issues/55)) ([5eb137a](https://github.com/meigma/blob/commit/5eb137ab3b007f259120b60f3a414d6ea50315ac))
* **core:** return CopyStats from copy operations ([#52](https://github.com/meigma/blob/issues/52)) ([c48479d](https://github.com/meigma/blob/commit/c48479d01f2e66015bc31de369f5eaab8962e416))

## 1.0.0 (2026-01-21)


### Features

* add block-level caching for ByteSource ([#7](https://github.com/meigma/blob/issues/7)) ([8fc2eef](https://github.com/meigma/blob/commit/8fc2eef5d98491bf322afa0e8feb9a5adb0e956c))
* add Client.Sign() method for sigstore manifest signing ([#22](https://github.com/meigma/blob/issues/22)) ([23a4065](https://github.com/meigma/blob/commit/23a4065bb31ae558c025cc827c85ae9b6bb2943f))
* add file-based API for local archives ([#5](https://github.com/meigma/blob/issues/5)) ([c126d80](https://github.com/meigma/blob/commit/c126d80386cda0318e771c5fe89ef2124956eb6e))
* add Inspect() method to fetch archive metadata without data blob ([#21](https://github.com/meigma/blob/issues/21)) ([3b0ac97](https://github.com/meigma/blob/commit/3b0ac97530abdf0f136ad38a0b120ff5a0935606))
* add optional structured logging with slog ([#42](https://github.com/meigma/blob/issues/42)) ([37fd027](https://github.com/meigma/blob/commit/37fd02715a0565f3abfc7f700fc8f2bd673c1e2c))
* adds cache integrity + validation to OCI caches ([#8](https://github.com/meigma/blob/issues/8)) ([cd1e58d](https://github.com/meigma/blob/commit/cd1e58d640818258724b23e3e20080c7ec516924))
* adds index caching for OCI images ([#9](https://github.com/meigma/blob/issues/9)) ([28b5908](https://github.com/meigma/blob/commit/28b5908fb8fed182535eff316ce63d5c08810dfc))
* adds landing page with benchmarks ([#15](https://github.com/meigma/blob/issues/15)) ([0f488ef](https://github.com/meigma/blob/commit/0f488ef2b4a2b0c0a4e13449c224bb74d55b092f))
* adds OCI client ([#6](https://github.com/meigma/blob/issues/6)) ([6974053](https://github.com/meigma/blob/commit/6974053f756feec487c4983991d7f613408a7ddf))
* adds policy engine with sigstore signing ([#13](https://github.com/meigma/blob/issues/13)) ([8cf67f9](https://github.com/meigma/blob/commit/8cf67f9a99dd0b24df7e6fa23497be7824f5b831))
* adds policy support for gittuf ([#36](https://github.com/meigma/blob/issues/36)) ([4ca8243](https://github.com/meigma/blob/commit/4ca824331bafd097f123aca60406c149b455d06c))
* adds TTL support for OCI ref cache ([#11](https://github.com/meigma/blob/issues/11)) ([5f900df](https://github.com/meigma/blob/commit/5f900df34c4eb0d8c687bf9cd78cb2764559d5ec))
* **examples:** integrate gittuf verification in provenance example ([#37](https://github.com/meigma/blob/issues/37)) ([24ae1ed](https://github.com/meigma/blob/commit/24ae1eda572ca602b338fb32f9d434aa8345a1b0))
* **examples:** update provenance to use new public API ([#20](https://github.com/meigma/blob/issues/20)) ([2160370](https://github.com/meigma/blob/commit/2160370d9c55784a8f5610c3e8a4724717360a5a))
* initial archive format design and interfaces ([#1](https://github.com/meigma/blob/issues/1)) ([6728a29](https://github.com/meigma/blob/commit/6728a29989f939ef0a472b9f4528f27d0cce2686))
* initial implementation of blob ([#2](https://github.com/meigma/blob/issues/2)) ([07ae9d3](https://github.com/meigma/blob/commit/07ae9d34c849e899fe20fd65dd2f929d29996583))


### Bug Fixes

* **ci:** check PR author instead of actor for dependabot detection ([#34](https://github.com/meigma/blob/issues/34)) ([d767490](https://github.com/meigma/blob/commit/d76749085580fb86e823b4651a59231bc54f0e9b))
* **ci:** use REPO_PAT to trigger CI on generated commits ([#35](https://github.com/meigma/blob/issues/35)) ([32c2430](https://github.com/meigma/blob/commit/32c2430fe8d289da8213d2ff3875dad2d55ffd9f))
* **gittuf:** remove WithAllowMissingGittuf() after adding SSH key to policy ([#43](https://github.com/meigma/blob/issues/43)) ([c08aa58](https://github.com/meigma/blob/commit/c08aa589c622466b183cff4797090e7632e05aba))
* **gittuf:** use forked gittuf instead of local /tmp path ([#40](https://github.com/meigma/blob/issues/40)) ([9dd6d80](https://github.com/meigma/blob/commit/9dd6d803aa49e9af7da7d19d025379ea59395f4f))
* prevents path traversal attacks by jailing extractions ([#19](https://github.com/meigma/blob/issues/19)) ([2350540](https://github.com/meigma/blob/commit/23505402268f24ab5c9121eca8317b2708968d51))


### Code Refactoring

* adds new public API ([#16](https://github.com/meigma/blob/issues/16)) ([dbcc376](https://github.com/meigma/blob/commit/dbcc3768190fb324a6b91199149d73a2985a0e3a))
