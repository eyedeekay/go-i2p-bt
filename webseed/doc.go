// Copyright 2020 go-i2p, 2023 idkpackage webseed

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package webseed implements BEP 19 (WebSeed - HTTP/FTP Seeding) support.
//
// BEP 19 specifies how to download pieces from HTTP/FTP servers (web seeds)
// in addition to regular BitTorrent peers. This allows content creators to
// provide reliable fallback sources for their torrents.
//
// The package provides WebSeedClient for HTTP range request downloads and
// WebSeedDownloadHandler for integration with the downloader package.
//
// Reference: https://www.bittorrent.org/beps/bep_0019.html
package webseed
