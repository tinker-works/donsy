package colima

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
)

const (
	// These are Linux CLONE_NEW* flags. The profile is built on macOS as well,
	// where the Linux syscall constants are not available to the Go compiler.
	nestedDockerNamespaceFlags      = 2114060288
	nestedDockerUserMountNamespaces = 268566528
	nestedDockerNetworkNamespace    = 1073741824
)

var nestedDockerSecurity = nestedDockerSeccompProfile()

// nestedDockerSeccompDefault is Docker's default seccomp profile, compressed
// to keep the complete policy beside the narrow RootlessKit exception below.
const nestedDockerSeccompDefault = `H4sICEVHh2oCA2RlZmF1bHQuanNvbgDVWtty2zgSfU6+wuXn1JYtaz3OvmlkZcY1vo2cudXWFIomQQkrgmDQoGxVKv++DVAiGiDtkSZS7ebBMs7BrQE0Gt0AP799c5zxPKkLM0qNUOXxv46OH8Y392w0/sgm0+nt3fE7X2aidamm3GCpU0snOp3fJBXCf7998+Yz/jWcMDw1tea+ten4R/b7xTk7H9r2sBjUjyNSEtZtYE5QoSkdkGeDY8v9iT9f3m3R7cj+7tTvaHqzWxc3V/cPO/VgK3SH1jRzu+sAfbX9CLB775PrnUc/ue7v3jb1t2ZgU3F/Yuwmw8PZ+5Pfd+jdlt+th+nVw/jX1ya6rIvCNvb2zZ92d8IK0qQoINieZSKpPEma8sps5qBBQwoBWpT9xwjJn1tcJFpuwKMoszatF5tkmqRzDiYxnqhmnCIgaJ4J7YFUmQfqqQzAZqERFipdsLVwveT5MKRRADtfPRx7oXi36TUZly2TUkHBefUC3e0AeACYTsqZp1RZ4vJ6WK1YLoq4lOZ+grO6IskBSZ9t0rxSRcFcLd7HnUakKToEU0UWktVTIkwPNQi5binLBM0teWnyGPpmnnm65CHyo+fPIkizmVZ+QvJGoX3xlhh4JlsKIEvUEoxyBS4rmb0c19eIfMVk4nU/D/Q5DxS6QUSQNR4Qgqh8Hul8g2n10i+SA0RYbDaBVZm2BOruc2KMF60QEDOotS1QZEyaS7XkYWGI26Mb3gEijYUdIocI0nwqutF1GUz8hiAVauNtlANM8081r3lIhjux4ah2emYZUwvaFDZDNArnNq1qip4ygjJUZoixlwIZPhNZBP26W6IO8+soP6we13bbATpEUEbYIWlCVJxre2xQKuymmumKwjAzglooLcyKUGjNMiUpwSHsoCECMZGq4zLxZOhCSEHXhmn1WINhVuNpuRqSGR0fBC0D7oZoAiylqqBpM0frmbEEfwltgpbs1CrckXT04SjiIQQ7a4O9vom14UmyDBXTpPM4Q5TC9HGnMall1IBiaVKmvPAEOQkwN8ODXasVYaw+LqmGI1e9TkbbUCirHoy4CGsGKGORt+oW149knUXVGoyFKFqRC9SytVuQMV0XvJPRHH8uj/TXZms7YJEa7L7I29zAUBeRoS7iFSxEuaBpv5JWJXlJkVR1GWRHLUGsDEWnEEP3j3PfY4/5DgvEtrygtrwIbblsjkYPKwbzJFNPDMv580Ny+ZhoLbxNQSbPIgek4YAj2/YnRZkq7YssyHnqgBdNLkp/ujpA8uhp5sAgQInXEYmDoGlfsNLKEFdMfrJKirNFJwvJZjMRQlV+URFadc80T7nwTkxER1tikwvce9iU6xavS6plEtecDAp44kcLM7KlEZF9h0inS4KAdE/PZInd0fltIJ3TuiQCWDvKjGJzu6s486vUcZ1L/rT2FlptRgr3HlkGOrs2nZgQtetXJbXXVDyQUNdo3YZx0wliVvopqkTFado3uOBoda0fGDDoIPGAiBXH+rxtugOixaw0WZ/KHi80D+EyRF665uSjpZX1dBlqQ8ETMhXNfJ7HOBbkCY/sDl5GsO3eCkPTyTwiqIJusF+7YGS4K4J0rr2rYLFE3YxxJL2jaSlrqlwcVeGxD4SPjSOhqHjUGWhQnEtUzx4bicZTownHW1pAumTzJ1yZR9+YJCZOA//Upo1VzKS5ows41OJMlLOIxOWWCSxC1nnBosxVSKPFrXXUKtRQEYPTkM7mUB85ouNpR2LW1y2kc55Z+5nkufVFVt0MsgYtWSU6kR2WbRxKDL6eX8sVZSfXJfC0j/rS2lUVpeF66W1Bb2Y05qYMvDQ06Bsa9A4NXhRvJbgPl/HETJWsPKTmEbgkBgQRMfCIFKkm3Sr2MfEIiVrYtN1HMQ5L0x3qcAiN1wuOoR9x/DfY+1OOqaMSdVQibCGuH4ZA0A2BIA6BIIx3oBvDgI0+wiKaxx2HQQ10gxqIgxroBjWOiot0SgRhD/SHPRBEOdAJaaA/pHG0yKwTrcnlIARxDERxTOxVQieOgbkMAFXbucxoFtXheW0y4n5bE1mYwPtsjnN/n7TBQ0LE1rJjE+3UYChPrHfDVIm31lAVIvU7gbgtodcc3nfE1x0WB36/JbxZW0l6cq4h6Yn4ZTbdc1loadL9CqhhNt55MTMaO9ELULc3Iu+94TI0PjGHq4UnqNZ12eG7jXZuVRsa+spCb9lOXLFh+/rzdLcV6C/f22s7nSaYs+iuqntVVXeuKGqqhjV1M0KnvkF+4WttjRJxJGoqvAMluaVqici218FolvTmDx2oQMHteT+kwO9+i8idj/MNA7Bs3j2ad4yk++44ur6+++04fBaJ3i02/uxSssgPbvnIRUVrg9Jv03GTjbFnUWeuz89tOPoT1yUvbJ3hPy5cY19eFbMxEzv0muiZr970a0XJ+DOyJ+/WBPodtX0cGraMqtom7d/txPXppIsfmOKZ5BoUmkR7nB1KzpfEnPz8fyXmxbch5unZ6cl3g29H1otvZPmHg/fD9+ffDd7/8+vljU5f7wk9JVWqMHZ4Nl9li+wrMenPXh+k58OiMXBOtr+wTImWrO9ty/KvuA59Y3pEC7yolChN8OybFzXMA7exgP2O2b8/u/T6xX670adz1tyu7Fci6W9o3hw/r79j2EYiqTJ7C19k5tAStemL862lg7P3J6xKBZNSKEavd8Kc4Kx3Wej0WKcC42VA/3+vQ7Ptt6Ox4Hnr8TSXL05DmXDaul/RXPs76KO9i2SPK3Ir+jXi2M8qiDDj0T27HI3ZdDK6ZA8T+y3J1oI9Vjn5PqHkATjrvH/T16Yc0NDlYuZxENjkQC9gc6iED9oKpRZ1xbIU/wv/TgPSXb3Y5xcWPlJIF9oy3ET27Yby0FchEMSB+GbGXvuxoJhbIkOvdzEgYO4VK7hK/lQrjD99+LrBLKfhcaZkIkrq4CM5V2AiqqShWqFmPlCgsjVo4KMDmGPMvm8devjjgY0ub65ut9aeRmUOdV4PTk+HJ+cng4uX/Lab0cNPk8vo2G564s9/Y8Dv9m+I9jpDp/ucoVRJjBHtp55uSEfuihLDe32kdMa1KGdHGB8euQEcCTjC4yvnmm+U8usN+Fcs1JbzfvYXE99+/2rl8J++nl0cVDTNH5Uyh9i839/dfdx+hub6QGKMf5zuIkhzq7S27f6rBWEiKqfcAcS+ubv85XqyvYuZpgeZvfvReLz95C1S/zbQPHLiCepPovZZMHzK/1/crrw03I/T0Xj7WReqIt+o4BEtDyHUdPTb1d32znPPdz/Q/aQU+r4z9feNBxjGx6ub7Wd2ib7prK4OIsfHP9j47vbD1Q9bS2P9QMllpQqR+s8t6FfI0FciINlcYWBSquwgint7tYParp27/YtxfffDTr7+niX4/v7D1t3H/vSeRbmfTD/c3HWO4beY/vJfBD2S5AczAAA=`

type seccompRule struct {
	Names  []string     `json:"names"`
	Action string       `json:"action"`
	Args   []seccompArg `json:"args,omitempty"`
}

type seccompArg struct {
	Index    int    `json:"index"`
	Value    uint64 `json:"value"`
	ValueTwo uint64 `json:"valueTwo,omitempty"`
	Op       string `json:"op"`
}

func nestedDockerSeccompProfile() string {
	compressed, err := base64.StdEncoding.DecodeString(nestedDockerSeccompDefault)
	if err != nil {
		panic(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		panic(err)
	}
	contents, err := io.ReadAll(reader)
	if err != nil {
		panic(err)
	}
	if err := reader.Close(); err != nil {
		panic(err)
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(contents, &profile); err != nil {
		panic(err)
	}
	var syscalls []json.RawMessage
	if err := json.Unmarshal(profile["syscalls"], &syscalls); err != nil {
		panic(err)
	}
	for _, rule := range []seccompRule{
		{
			Names: []string{"clone"}, Action: "SCMP_ACT_ALLOW",
			Args: []seccompArg{{
				Index: 0, Value: nestedDockerNamespaceFlags,
				ValueTwo: nestedDockerUserMountNamespaces, Op: "SCMP_CMP_MASKED_EQ",
			}},
		},
		{
			Names: []string{"unshare"}, Action: "SCMP_ACT_ALLOW",
			Args: []seccompArg{{
				Index: 0, Value: nestedDockerNamespaceFlags,
				ValueTwo: nestedDockerNetworkNamespace, Op: "SCMP_CMP_MASKED_EQ",
			}},
		},
	} {
		encoded, err := json.Marshal(rule)
		if err != nil {
			panic(err)
		}
		syscalls = append(syscalls, encoded)
	}
	encoded, err := json.Marshal(syscalls)
	if err != nil {
		panic(err)
	}
	profile["syscalls"] = encoded
	encoded, err = json.Marshal(profile)
	if err != nil {
		panic(err)
	}
	return "seccomp=" + string(encoded)
}
