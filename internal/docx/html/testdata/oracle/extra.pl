use strict; use warnings; use JSON::PP;
local $/; my $d = decode_json(<STDIN>);
my @extra = (
 '<p>a<b>b<p>c</b>d', '<table><tr><td>x<td>y</table>', '<table>x<tr><td>a</table>', '<p><span><b>x</span>y</p>',
 '<ul><li>a<li>b</ul>', '<p><math><mi>x</mi><mspace width="1em"/></math></p>', '<p>a &amp; &copy &nbsp;</p>', '<p>a &nbsp;&notin; AT&T &#x41;</p>',
 '<p><a href="?a=1&copy=2">x</a></p>', '<p><image src=x></image></p>', '<p><span/>x</p>', '<p>x</p></p>', '<p>x<li>y', '<ol start=3><li>a<ol><li>b</ol><li>c</ol>',
 '<p>list-id="1"</p>', '<ul><li>one<p>two</p><p>three</p></li></ul>', '<table><td>a</table>', '<div>a<p>b</p></div>', '<blockquote>q</blockquote>',
 '<p><!-- c -->x</p>', '<p>x<!-->y</p>', '<p>&bogus;</p>', '<p>&#0;</p>', '<p>&#x110000;</p>', '<p>&amp</p>', '<p title=&amp>x</p>',
 '<p a=b c=d>x</p>', "<p a='b'>x</p>", '<p a="b"c="d">x</p>', '<P CLASS="Title">x</P>', '<p>x</P>', '<h1>a</h2>', '<h1>a<h2>b</h2>',
 '<!DOCTYPE html><html><head><meta charset="utf-8"><title>t</title></head><body><p>x</p></body></html>',
 '<html><body><p>x</p></body></html>', '<!doctype html><p>x</p>', '<body><p>x</p></body>', '<p>a</p>text', '<p>a</p>  ', 'plain text',
 '<p>a<br>b<br/>c</br></p>', '<p><br type="page"></p>', '<p><img src="data:image/png;base64,AA" width=10 height=20 alt=x></p>',
 '<p><mark>x</mark><mark data-highlight="green">y</mark></p>', '<p><mark data-highlight="nope">x</mark></p>',
 '<p style="text-align:center;color:#f00">x</p>', '<p style="bogus:1">x</p>', '<p><span style="font-size:13pt">x</span></p>',
 '<p><b><i>x</i></b></p>', '<p><b><b>x</b></b></p>', '<p><ins>a</ins><del data-docx-revision="7">b</del></p>', '<p><ins><ins>a</ins></ins></p>',
 '<p><field instr="PAGE">1</field></p>', '<p><field>1</field></p>', '<p><a href="javascript:x">x</a></p>', '<p><a>x</a></p>',
 '<p><footnote>note</footnote></p>', '<p>x<footnote><p>a</p><p>b</p></footnote></p>', '<section><p>x</p></section>',
 '<!DOCTYPE html><html><body><header><p>h</p></header><p>x</p><section first-page/><footer even><p>f</p></footer><p>y</p></body></html>',
 '<!DOCTYPE html><html><body><p>x</p><header><p>h</p></header></body></html>', '<!DOCTYPE html><html><body><header><p>h</p></header></body></html>',
 '<p><page-number/> of <page-count/></p>', '<p><comment-start id="1"/>x<comment-end id="1"/></p>', '<p><span data-docx-comment-start="2"></span>x</p>',
 '<table style="x"><tr><td>a</td></tr></table>', '<table><tr><th colspan=2>a</th></tr></table>', '<table><tr><td><p>a</p><p>b</p></td></tr></table>',
 '<p><math><mfrac><mi>a</mi><mi>b</mi></mfrac></math></p>', '<p><math display="block"><mi>x</mi></math></p>', '<math><mi>x</mi></math>',
 '<p><math><mi>x</mi><b>y</b></math></p>', '<p>a<div>b</div></p>', '<p>a<table><tr><td>b</td></tr></table></p>', '<p onclick="x">a</p>',
 '<script>x</script>', '<p><code>x</code></p>', '<pre>x</pre>', '<p>a<sup>2</sup><sub>i</sub></p>', '<p><s>x</s><strike>y</strike><u>z</u></p>',
 '<p><span pending>x</span></p>', '<p><format pending>x</format></p>', '<p><b pending>x</b></p>', '<div><p>a</p>b</div>', '<ul><li></li></ul>',
 '<p>tab	here</p>', "<p>line\nbreak</p>", "<p>cr\r\nlf</p>", '<p>&lt;b&gt;</p>', '<p>a</p><p>b</p>', '<h3 class="X" id="a">t</h3>',
 '<p><unknown>x</unknown></p>', '<p><equation>\frac{a}{b}</equation></p>', '<p><equation><math><mi>x</mi></math></equation></p>',
 '<ul><li>a list-id="1" b<p>c</p></li></ul>', '<ul><li>x<p>y</p><p>z</p></li><li>w</li></ul>', '<p title="<section/>">x</p>', '<p class="a" class="b">x</p>', '<p><b style="color:#f00" style="color:#0f0">x</b></p>', '<p><comment-start id="1"/>x<comment-end id="1"/><page-number/></p>', '<!DOCTYPE html><html><body><header first/><footer/><p>x</p></body></html>',
 "<p>a\r\n&bogus;</p>", "<p>a\r\nb</p>", '<ol type="a"><li>x</li></ol>', '<ol type="q"><li>x</li></ol>', '<ol start="0"><li>x</li></ol>', '<ol start="+2"><li>x</li></ol>',
);
my @out = @$d; my %seen = map { $_->{input} => 1 } @out;
for my $s (@extra) { next if $seen{$s}++; push @out, {input=>$s, source=>'hand'}; }
srand(42); my @base = @out;
for my $c (@base) {
  my $s = $c->{input}; my $n = length $s; next if $n < 4;
  for (1..3) {
    my $p = int(rand($n)); my $m;
    my $k = int(rand(3));
    if ($k == 0) { $m = substr($s, 0, $p) } elsif ($k == 1) { $m = substr($s, 0, $p) . substr($s, $p+1) } else { $m = substr($s,0,$p) . substr($s,$p,int(rand(8))+1) . substr($s,$p) }
    next if $seen{$m}++; push @out, {input=>$m, source=>'mutation of '.$c->{source}};
  }
}
print JSON::PP->new->utf8->canonical->encode(\@out);
