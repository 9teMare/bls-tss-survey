import sys
import math
import subprocess


def run_ibm(n: int):
    print(f"Runing IBM BLS-TSS benchmark: n={n}, t={math.ceil(int(n) / 2)}")
    subprocess.run(
        f"cd IBM-TSS/test/tbls && N={n} go test -run TestBenchmark -v",
        shell=True,
        check=True,
    )


def run_coinbase(n: int):
    print(f"Runing Coinbase BLS-TSS benchmark: n={n}, t={math.ceil(int(n) / 2)}")
    subprocess.run(
        f"cd Coinbase-Kryptology/pkg/signatures/bls/bls_sig && N={n} go test -run ^TestBasicThresholdKeygen$ -v",
        shell=True,
        check=True,
    )


def helper():
    print("Usage: python3 test.py lib=<library> n=<threshold>")
    print("library: ibm or coinbase")
    print("threshold: odd integer > 2")


if __name__ == "__main__":
    if (
        len(sys.argv) != 3
        or not sys.argv[1].startswith("lib=")
        or not sys.argv[2].startswith("n=")
        or sys.argv[1] == "-h"
    ):
        helper()
        sys.exit(1)

    lib = sys.argv[1].split("=")[1]  # ibm or coinbase
    if lib != "ibm" and lib != "coinbase":
        print("Invalid library name, must be 'ibm' or 'coinbase'")
        sys.exit(1)

    n = sys.argv[2].split("=")[1]
    if not n.isdigit():
        print("Invalid n value, must be an integer")
        sys.exit(1)
    if int(n) < 3 or int(n) % 2 != 1:
        print("Invalid n value, must be an odd integer greater than 2")
        sys.exit(1)

    if lib == "ibm":
        run_ibm(int(n))
    else:
        run_coinbase(int(n))
