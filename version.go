package main

// appVersion 是人类可读的递增版本号（主.次.修订），每次改动对外行为或 API 时手工递增。
// build-run.sh 会从本行解析源码版本，并与编译产物、运行中实例的版本逐一比对，
// 因此格式必须保持为 `const appVersion = "x.y.z"`。
//
// 版本历史：
//
//	1.1.0  新增 PUT /api/tabs 幂等登记（registrationKey）、GET /api/version、--version
//	1.0.0  初始版本（仓库标签 1.0）
const appVersion = "1.1.0"
