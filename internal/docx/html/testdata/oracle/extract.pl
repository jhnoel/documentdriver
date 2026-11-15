#!/usr/bin/perl
# Collect HTML-looking string literals from Rust/TS sources and Markdown docs.
use strict; use warnings; use JSON::PP;
my $root = shift;
my (%seen, @out);
my $tagre = qr/<\/?(p|h[1-6]|div|span|b|i|u|s|strong|em|sup|sub|a|br|img|image|table|tr|td|th|tbody|thead|ul|ol|li|ins|del|mark|math|mi|mn|mo|mrow|equation|field|format|footnote|endnote|section|header|footer|comment-start|comment-end|page-number|page-count|html|body|head|blockquote|strike|main|article|!doctype)\b/i;
sub add { my ($s, $src) = @_; return unless $s =~ $tagre; return if length($s) > 20000; return if $seen{$s}++; push @out, {input=>$s, source=>$src}; }
sub unesc_rust { my $s = shift; $s =~ s/\\\n\s*//g; $s =~ s/\\u\{([0-9a-fA-F]+)\}/chr(hex($1))/ge; $s =~ s/\\(["\\nrt0'])/{'"'=>'"','\\'=>'\\','n'=>"\n",'r'=>"\r",'t'=>"\t",'0'=>"\0","'"=>"'"}->{$1}/ge; return $s; }
my @files = split /\n/, `cd $root && git ls-files 'crates/*.rs' 'packages/docxdriver-pi/skills/*.md' 'packages/docxdriver/README.md' 'docs/*.md' 'README.md' 'packages/*/test/*.ts' 'packages/*/test/*.mjs' 'packages/*/src/*.test.ts'`;
for my $f (@files) {
  open my $fh, '<:encoding(UTF-8)', "$root/$f" or next; local $/; my $t = <$fh>; close $fh;
  if ($f =~ /\.rs$/) {
    while ($t =~ /r(#+)"(.*?)"\1/gs) { add($2, $f); }
    my $t2 = $t; $t2 =~ s/r(#+)".*?"\1//gs; $t2 =~ s/r"[^"]*"//gs;
    while ($t2 =~ /r"([^"]*)"/gs) { add($1, $f); }
    while ($t2 =~ /(?<![\w'])"((?:[^"\\]|\\.)*)"/gs) { add(unesc_rust($1), $f); }
  } elsif ($f =~ /\.(ts|mjs)$/) {
    while ($t =~ /`((?:[^`\\]|\\.)*)`/gs) { my $s=$1; next if $s =~ /\$\{/; $s =~ s/\\(.)/$1/g; add($s, $f); }
    while ($t =~ /(["'])((?:(?!\1)[^\\]|\\.)*)\1/g) { my $s=$2; $s =~ s/\\n/\n/g; $s =~ s/\\(.)/$1/g; add($s, $f); }
  } else {
    while ($t =~ /```[a-z]*\n(.*?)```/gs) { add($1, $f); }
    while ($t =~ /(?<!`)`([^`\n]+)`(?!`)/g) { add($1, $f); }
  }
}
print JSON::PP->new->utf8->canonical->pretty->encode(\@out);
