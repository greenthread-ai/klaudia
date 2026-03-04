var emq, Wa6;
var Jh1 = E(() => {
  ((emq =
    typeof global == "object" && global && global.Object === Object && global),
    (Wa6 = emq));
});
var ABq, qBq, lH;
var JL = E(() => {
  Jh1();
  ((ABq = typeof self == "object" && self && self.Object === Object && self),
    (qBq = Wa6 || ABq || Function("return this")()),
    (lH = qBq));
});
var KBq, aX;
var TA6 = E(() => {
  JL();
  ((KBq = lH.Symbol), (aX = KBq));
});
function wBq(A) {
  var q = YBq.call(A, Jk6),
    K = A[Jk6];
  try {
    A[Jk6] = void 0;
    var Y = !0;
  } catch (w) {}
  var z = zBq.call(A);
  if (Y)
    if (q) A[Jk6] = K;
    else delete A[Jk6];
  return z;
}
var Oo8, YBq, zBq, Jk6, Ho8;
var jo8 = E(() => {
  TA6();
  ((Oo8 = Object.prototype),
    (YBq = Oo8.hasOwnProperty),
    (zBq = Oo8.toString),
    (Jk6 = aX ? aX.toStringTag : void 0));
  Ho8 = wBq;
});
function OBq(A) {
  return $Bq.call(A);
}
var _Bq, $Bq, Jo8;
var Do8 = E(() => {
  ((_Bq = Object.prototype), ($Bq = _Bq.toString));
  Jo8 = OBq;
});
function JBq(A) {
  if (A == null) return A === void 0 ? jBq : HBq;
  return Xo8 && Xo8 in Object(A) ? Ho8(A) : Jo8(A);
}
var HBq = "[object Null]",
  jBq = "[object Undefined]",
  Xo8,
  Zv;
var NA6 = E(() => {
  TA6();
  jo8();
  Do8();
  Xo8 = aX ? aX.toStringTag : void 0;
  Zv = JBq;
});
function DBq(A) {
  var q = typeof A;
  return A != null && (q == "object" || q == "function");
}
var O2;
var yZ = E(() => {
  O2 = DBq;
});
function GBq(A) {
  if (!O2(A)) return !1;
  var q = Zv(A);
  return q == MBq || q == PBq || q == XBq || q == WBq;
}
var XBq = "[object AsyncFunction]",
  MBq = "[object Function]",
  PBq = "[object GeneratorFunction]",
  WBq = "[object Proxy]",
  P_6;
var Ga6 = E(() => {
  NA6();
  yZ();
  P_6 = GBq;
});
var ZBq, Za6;
var Mo8 = E(() => {
  JL();
  ((ZBq = lH["__core-js_shared__"]), (Za6 = ZBq));
});
function fBq(A) {
  return !!Po8 && Po8 in A;
}
var Po8, Wo8;
var Go8 = E(() => {
  Mo8();
  Po8 = (function () {
    var A = /[^.]+$/.exec((Za6 && Za6.keys && Za6.keys.IE_PROTO) || "");
    return A ? "Symbol(src)_1." + A : "";
  })();
  Wo8 = fBq;
});
function VBq(A) {
  if (A != null) {
    try {
      return NBq.call(A);
    } catch (q) {}
    try {
      return A + "";
    } catch (q) {}
  }
  return "";
}
var TBq, NBq, np;
var Dh1 = E(() => {
  ((TBq = Function.prototype), (NBq = TBq.toString));
  np = VBq;
});
function SBq(A) {
  if (!O2(A) || Wo8(A)) return !1;
  var q = P_6(A) ? CBq : kBq;
  return q.test(np(A));
}
var vBq, kBq, EBq, LBq, yBq, RBq, CBq, Zo8;
var fo8 = E(() => {
  Ga6();
  Go8();
  yZ();
  Dh1();
  ((vBq = /[\\^$.*+?()[\]{}|]/g),
    (kBq = /^\[object .+?Constructor\]$/),
    (EBq = Function.prototype),
    (LBq = Object.prototype),
    (yBq = EBq.toString),
    (RBq = LBq.hasOwnProperty),
    (CBq = RegExp(
      "^" +
        yBq
          .call(RBq)
          .replace(vBq, "\\$&")
          .replace(
            /hasOwnProperty|(function).*?(?=\\\()| for .+?(?=\\\])/g,
            "$1.*?",
          ) +
        "$",
    )));
  Zo8 = SBq;
});
function hBq(A, q) {
  return A == null ? void 0 : A[q];
}
var To8;
var No8 = E(() => {
  To8 = hBq;
});
function IBq(A, q) {
  var K = To8(A, q);
  return Zo8(K) ? K : void 0;
}
var uT;
var gn = E(() => {
  fo8();
  No8();
  uT = IBq;
});
var xBq, rp;
var Dk6 = E(() => {
  gn();
  ((xBq = uT(Object, "create")), (rp = xBq));
});
function bBq() {
  ((this.__data__ = rp ? rp(null) : {}), (this.size = 0));
}
var Vo8;
var vo8 = E(() => {
  Dk6();
  Vo8 = bBq;
});
function uBq(A) {
  var q = this.has(A) && delete this.__data__[A];
  return ((this.size -= q ? 1 : 0), q);
}
var ko8;
var Eo8 = E(() => {
  ko8 = uBq;
});
function FBq(A) {
  var q = this.__data__;
  if (rp) {
    var K = q[A];
    return K === mBq ? void 0 : K;
  }
  return gBq.call(q, A) ? q[A] : void 0;
}
var mBq = "__lodash_hash_undefined__",
  BBq,
  gBq,
  Lo8;
var yo8 = E(() => {
  Dk6();
  ((BBq = Object.prototype), (gBq = BBq.hasOwnProperty));
  Lo8 = FBq;
});
function UBq(A) {
  var q = this.__data__;
  return rp ? q[A] !== void 0 : QBq.call(q, A);
}
var pBq, QBq, Ro8;
var Co8 = E(() => {
  Dk6();
  ((pBq = Object.prototype), (QBq = pBq.hasOwnProperty));
  Ro8 = UBq;
});
function cBq(A, q) {
  var K = this.__data__;
  return (
    (this.size += this.has(A) ? 0 : 1),
    (K[A] = rp && q === void 0 ? dBq : q),
    this
  );
}
var dBq = "__lodash_hash_undefined__",
  So8;
var ho8 = E(() => {
  Dk6();
  So8 = cBq;
});
function W_6(A) {
  var q = -1,
    K = A == null ? 0 : A.length;
  this.clear();
  while (++q < K) {
    var Y = A[q];
    this.set(Y[0], Y[1]);
  }
}
var Xh1;
var Io8 = E(() => {
  vo8();
  Eo8();
  yo8();
  Co8();
  ho8();
  W_6.prototype.clear = Vo8;
  W_6.prototype.delete = ko8;
  W_6.prototype.get = Lo8;
  W_6.prototype.has = Ro8;
  W_6.prototype.set = So8;
  Xh1 = W_6;
});
function lBq() {
  ((this.__data__ = []), (this.size = 0));
}
var xo8;
var bo8 = E(() => {
  xo8 = lBq;
});
function iBq(A, q) {
  return A === q || (A !== A && q !== q);
}
var db;
var G_6 = E(() => {
  db = iBq;
});
function nBq(A, q) {
  var K = A.length;
  while (K--) if (db(A[K][0], q)) return K;
  return -1;
}
var Fn;
var Xk6 = E(() => {
  G_6();
  Fn = nBq;
});
function aBq(A) {
  var q = this.__data__,
    K = Fn(q, A);
  if (K < 0) return !1;
  var Y = q.length - 1;
  if (K == Y) q.pop();
  else oBq.call(q, K, 1);
  return (--this.size, !0);
}
var rBq, oBq, uo8;
var mo8 = E(() => {
  Xk6();
  ((rBq = Array.prototype), (oBq = rBq.splice));
  uo8 = aBq;
});
function sBq(A) {
  var q = this.__data__,
    K = Fn(q, A);
  return K < 0 ? void 0 : q[K][1];
}
var Bo8;
var go8 = E(() => {
  Xk6();
  Bo8 = sBq;
});
function tBq(A) {
  return Fn(this.__data__, A) > -1;
}
var Fo8;
var po8 = E(() => {
  Xk6();
  Fo8 = tBq;
});
function eBq(A, q) {
  var K = this.__data__,
    Y = Fn(K, A);
  if (Y < 0) (++this.size, K.push([A, q]));
  else K[Y][1] = q;
  return this;
}
var Qo8;
var Uo8 = E(() => {
  Xk6();
  Qo8 = eBq;
});
function Z_6(A) {
  var q = -1,
    K = A == null ? 0 : A.length;
  this.clear();
  while (++q < K) {
    var Y = A[q];
    this.set(Y[0], Y[1]);
  }
}
var pn;
var Mk6 = E(() => {
  bo8();
  mo8();
  go8();
  po8();
  Uo8();
  Z_6.prototype.clear = xo8;
  Z_6.prototype.delete = uo8;
  Z_6.prototype.get = Bo8;
  Z_6.prototype.has = Fo8;
  Z_6.prototype.set = Qo8;
  pn = Z_6;
});
var Agq, Qn;
var fa6 = E(() => {
  gn();
  JL();
  ((Agq = uT(lH, "Map")), (Qn = Agq));
});
function qgq() {
  ((this.size = 0),
    (this.__data__ = {
      hash: new Xh1(),
      map: new (Qn || pn)(),
      string: new Xh1(),
    }));
}
var do8;
var co8 = E(() => {
  Io8();
  Mk6();
  fa6();
  do8 = qgq;
});
function Kgq(A) {
  var q = typeof A;
  return q == "string" || q == "number" || q == "symbol" || q == "boolean"
    ? A !== "__proto__"
    : A === null;
}
var lo8;
var io8 = E(() => {
  lo8 = Kgq;
});
function Ygq(A, q) {
  var K = A.__data__;
  return lo8(q) ? K[typeof q == "string" ? "string" : "hash"] : K.map;
}
var Un;
var Pk6 = E(() => {
  io8();
  Un = Ygq;
});
function zgq(A) {
  var q = Un(this, A).delete(A);
  return ((this.size -= q ? 1 : 0), q);
}
var no8;
var ro8 = E(() => {
  Pk6();
  no8 = zgq;
});
function wgq(A) {
  return Un(this, A).get(A);
}
var oo8;
var ao8 = E(() => {
  Pk6();
  oo8 = wgq;
});
function _gq(A) {
  return Un(this, A).has(A);
}
var so8;
var to8 = E(() => {
  Pk6();
  so8 = _gq;
});
function $gq(A, q) {
  var K = Un(this, A),
    Y = K.size;
  return (K.set(A, q), (this.size += K.size == Y ? 0 : 1), this);
}
var eo8;
var Aa8 = E(() => {
  Pk6();
  eo8 = $gq;
});
function f_6(A) {
  var q = -1,
    K = A == null ? 0 : A.length;
  this.clear();
  while (++q < K) {
    var Y = A[q];
    this.set(Y[0], Y[1]);
  }
}
var VA6;
var Ta6 = E(() => {
  co8();
  ro8();
  ao8();
  to8();
  Aa8();
  f_6.prototype.clear = do8;
  f_6.prototype.delete = no8;
  f_6.prototype.get = oo8;
  f_6.prototype.has = so8;
  f_6.prototype.set = eo8;
  VA6 = f_6;
});
function Mh1(A, q) {
  if (typeof A != "function" || (q != null && typeof q != "function"))
    throw TypeError(Ogq);
  var K = function () {
    var Y = arguments,
      z = q ? q.apply(this, Y) : Y[0],
      w = K.cache;
    if (w.has(z)) return w.get(z);
    var _ = A.apply(this, Y);
    return ((K.cache = w.set(z, _) || w), _);
  };
  return ((K.cache = new (Mh1.Cache || VA6)()), K);
}
var Ogq = "Expected a function",
  T8;
var Sq = E(() => {
  Ta6();
  Mh1.Cache = VA6;
  T8 = Mh1;
});
function qa8(A) {
  return (q) => {
    if (q.code === "EPIPE") A.destroy();
  };
}
function Ka8() {
  (process.stdout.on("error", qa8(process.stdout)),
    process.stderr.on("error", qa8(process.stderr)));
}
function Ya8(A, q) {
  if (A.destroyed) return;
  A.write(q);
}
function L4(A) {
  Ya8(process.stdout, A);
}
function dn(A) {
  Ya8(process.stderr, A);
}
function Hgq(A) {
  let q = [],
    K = A.match(/^MCP server ["']([^"']+)["']/);
  if (K && K[1]) (q.push("mcp"), q.push(K[1].toLowerCase()));
  else {
    let w = A.match(/^([^:[]+):/);
    if (w && w[1]) q.push(w[1].trim().toLowerCase());
  }
  let Y = A.match(/^\[([^\]]+)]/);
  if (Y && Y[1]) q.push(Y[1].trim().toLowerCase());
  if (A.toLowerCase().includes("1p event:")) q.push("1p");
  let z = A.match(/:\s*([^:]+?)(?:\s+(?:type|mode|status|event))?:/);
  if (z && z[1]) {
    let w = z[1].trim().toLowerCase();
    if (w.length < 30 && !w.includes(" ")) q.push(w);
  }
  return Array.from(new Set(q));
}
function jgq(A, q) {
  if (!q) return !0;
  if (A.length === 0) return !1;
  if (q.isExclusive) return !A.some((K) => q.exclude.includes(K));
  else return A.some((K) => q.include.includes(K));
}
function wa8(A, q) {
  if (!q) return !0;
  let K = Hgq(A);
  return jgq(K, q);
}
var za8;
var _a8 = E(() => {
  Sq();
  za8 = T8((A) => {
    if (!A || A.trim() === "") return null;
    let q = A.split(",")
      .map((w) => w.trim())
      .filter(Boolean);
    if (q.length === 0) return null;
    let K = q.some((w) => w.startsWith("!")),
      Y = q.some((w) => !w.startsWith("!"));
    if (K && Y) return null;
    let z = q.map((w) => w.replace(/^!/, "").toLowerCase());
    return { include: K ? [] : z, exclude: K ? z : [], isExclusive: K };
  });
});
function Jgq() {
  ((this.__data__ = new pn()), (this.size = 0));
}
var $a8;
var Oa8 = E(() => {
  Mk6();
  $a8 = Jgq;
});
function Dgq(A) {
  var q = this.__data__,
    K = q.delete(A);
  return ((this.size = q.size), K);
}
var Ha8;
var ja8 = E(() => {
  Ha8 = Dgq;
});
function Xgq(A) {
  return this.__data__.get(A);
}
var Ja8;
var Da8 = E(() => {
  Ja8 = Xgq;
});
function Mgq(A) {
  return this.__data__.has(A);
}
var Xa8;
var Ma8 = E(() => {
  Xa8 = Mgq;
});
function Wgq(A, q) {
  var K = this.__data__;
  if (K instanceof pn) {
    var Y = K.__data__;
    if (!Qn || Y.length < Pgq - 1)
      return (Y.push([A, q]), (this.size = ++K.size), this);
    K = this.__data__ = new VA6(Y);
  }
  return (K.set(A, q), (this.size = K.size), this);
}
var Pgq = 200,
  Pa8;
var Wa8 = E(() => {
  Mk6();
  fa6();
  Ta6();
  Pa8 = Wgq;
});
function T_6(A) {
  var q = (this.__data__ = new pn(A));
  this.size = q.size;
}
var cb;
var Wk6 = E(() => {
  Mk6();
  Oa8();
  ja8();
  Da8();
  Ma8();
  Wa8();
  T_6.prototype.clear = $a8;
  T_6.prototype.delete = Ha8;
  T_6.prototype.get = Ja8;
  T_6.prototype.has = Xa8;
  T_6.prototype.set = Pa8;
  cb = T_6;
});
function Zgq(A) {
  return (this.__data__.set(A, Ggq), this);
}
var Ggq = "__lodash_hash_undefined__",
  Ga8;
var Za8 = E(() => {
  Ga8 = Zgq;
});
function fgq(A) {
  return this.__data__.has(A);
}
var fa8;
var Ta8 = E(() => {
  fa8 = fgq;
});
function Na6(A) {
  var q = -1,
    K = A == null ? 0 : A.length;
  this.__data__ = new VA6();
  while (++q < K) this.add(A[q]);
}
var Va6;
var Ph1 = E(() => {
  Ta6();
  Za8();
  Ta8();
  Na6.prototype.add = Na6.prototype.push = Ga8;
  Na6.prototype.has = fa8;
  Va6 = Na6;
});
function Tgq(A, q) {
  var K = -1,
    Y = A == null ? 0 : A.length;
  while (++K < Y) if (q(A[K], K, A)) return !0;
  return !1;
}
var Na8;
var Va8 = E(() => {
  Na8 = Tgq;
});
function Ngq(A, q) {
  return A.has(q);
}
var va6;
var Wh1 = E(() => {
  va6 = Ngq;
});
function kgq(A, q, K, Y, z, w) {
  var _ = K & Vgq,
    $ = A.length,
    O = q.length;
  if ($ != O && !(_ && O > $)) return !1;
  var H = w.get(A),
    j = w.get(q);
  if (H && j) return H == q && j == A;
  var J = -1,
    D = !0,
    X = K & vgq ? new Va6() : void 0;
  (w.set(A, q), w.set(q, A));
  while (++J < $) {
    var M = A[J],
      P = q[J];
    if (Y) var W = _ ? Y(P, M, J, q, A, w) : Y(M, P, J, A, q, w);
    if (W !== void 0) {
      if (W) continue;
      D = !1;
      break;
    }
    if (X) {
      if (
        !Na8(q, function (G, Z) {
          if (!va6(X, Z) && (M === G || z(M, G, K, Y, w))) return X.push(Z);
        })
      ) {
        D = !1;
        break;
      }
    } else if (!(M === P || z(M, P, K, Y, w))) {
      D = !1;
      break;
    }
  }
  return (w.delete(A), w.delete(q), D);
}
var Vgq = 1,
  vgq = 2,
  ka6;
var Gh1 = E(() => {
  Ph1();
  Va8();
  Wh1();
  ka6 = kgq;
});
var Egq, N_6;
var Zh1 = E(() => {
  JL();
  ((Egq = lH.Uint8Array), (N_6 = Egq));
});
function Lgq(A) {
  var q = -1,
    K = Array(A.size);
  return (
    A.forEach(function (Y, z) {
      K[++q] = [z, Y];
    }),
    K
  );
}
var va8;
var ka8 = E(() => {
  va8 = Lgq;
});
function ygq(A) {
  var q = -1,
    K = Array(A.size);
  return (
    A.forEach(function (Y) {
      K[++q] = Y;
    }),
    K
  );
}
var V_6;
var Ea6 = E(() => {
  V_6 = ygq;
});
function Qgq(A, q, K, Y, z, w, _) {
  switch (K) {
    case pgq:
      if (A.byteLength != q.byteLength || A.byteOffset != q.byteOffset)
        return !1;
      ((A = A.buffer), (q = q.buffer));
    case Fgq:
      if (A.byteLength != q.byteLength || !w(new N_6(A), new N_6(q))) return !1;
      return !0;
    case Sgq:
    case hgq:
    case bgq:
      return db(+A, +q);
    case Igq:
      return A.name == q.name && A.message == q.message;
    case ugq:
    case Bgq:
      return A == q + "";
    case xgq:
      var $ = va8;
    case mgq:
      var O = Y & Rgq;
      if (($ || ($ = V_6), A.size != q.size && !O)) return !1;
      var H = _.get(A);
      if (H) return H == q;
      ((Y |= Cgq), _.set(A, q));
      var j = ka6($(A), $(q), Y, z, w, _);
      return (_.delete(A), j);
    case ggq:
      if (fh1) return fh1.call(A) == fh1.call(q);
  }
  return !1;
}
var Rgq = 1,
  Cgq = 2,
  Sgq = "[object Boolean]",
  hgq = "[object Date]",
  Igq = "[object Error]",
  xgq = "[object Map]",
  bgq = "[object Number]",
  ugq = "[object RegExp]",
  mgq = "[object Set]",
  Bgq = "[object String]",
  ggq = "[object Symbol]",
  Fgq = "[object ArrayBuffer]",
  pgq = "[object DataView]",
  Ea8,
  fh1,
  La8;
var ya8 = E(() => {
  TA6();
  Zh1();
  G_6();
  Gh1();
  ka8();
  Ea6();
  ((Ea8 = aX ? aX.prototype : void 0), (fh1 = Ea8 ? Ea8.valueOf : void 0));
  La8 = Qgq;
});
function Ugq(A, q) {
  var K = -1,
    Y = q.length,
    z = A.length;
  while (++K < Y) A[z + K] = q[K];
  return A;
}
var v_6;
var La6 = E(() => {
  v_6 = Ugq;
});
var dgq, H2;
var RZ = E(() => {
  ((dgq = Array.isArray), (H2 = dgq));
});
function cgq(A, q, K) {
  var Y = q(A);
  return H2(A) ? Y : v_6(Y, K(A));
}
var ya6;
var Th1 = E(() => {
  La6();
  RZ();
  ya6 = cgq;
});
function lgq(A, q) {
  var K = -1,
    Y = A == null ? 0 : A.length,
    z = 0,
    w = [];
  while (++K < Y) {
    var _ = A[K];
    if (q(_, K, A)) w[z++] = _;
  }
  return w;
}
var Ra6;
var Nh1 = E(() => {
  Ra6 = lgq;
});
function igq() {
  return [];
}
var Ca6;
var Vh1 = E(() => {
  Ca6 = igq;
});
var ngq, rgq, Ra8, ogq, k_6;
var Sa6 = E(() => {
  Nh1();
  Vh1();
  ((ngq = Object.prototype),
    (rgq = ngq.propertyIsEnumerable),
    (Ra8 = Object.getOwnPropertySymbols),
    (ogq = !Ra8
      ? Ca6
      : function (A) {
          if (A == null) return [];
          return (
            (A = Object(A)),
            Ra6(Ra8(A), function (q) {
              return rgq.call(A, q);
            })
          );
        }),
    (k_6 = ogq));
});
function agq(A, q) {
  var K = -1,
    Y = Array(A);
  while (++K < A) Y[K] = q(K);
  return Y;
}
var Ca8;
var Sa8 = E(() => {
  Ca8 = agq;
});
function sgq(A) {
  return A != null && typeof A == "object";
}
var oD;
var lb = E(() => {
  oD = sgq;
});
function egq(A) {
  return oD(A) && Zv(A) == tgq;
}
var tgq = "[object Arguments]",
  vh1;
var ha8 = E(() => {
  NA6();
  lb();
  vh1 = egq;
});
var Ia8, AFq, qFq, KFq, op;
var Gk6 = E(() => {
  ha8();
  lb();
  ((Ia8 = Object.prototype),
    (AFq = Ia8.hasOwnProperty),
    (qFq = Ia8.propertyIsEnumerable),
    (KFq = vh1(
      (function () {
        return arguments;
      })(),
    )
      ? vh1
      : function (A) {
          return oD(A) && AFq.call(A, "callee") && !qFq.call(A, "callee");
        }),
    (op = KFq));
});
function YFq() {
  return !1;
}
var xa8;
var ba8 = E(() => {
  xa8 = YFq;
});
var Ia6 = {};
s1(Ia6, { default: () => ib });
var Ba8, ua8, zFq, ma8, wFq, _Fq, ib;
var Zk6 = E(() => {
  JL();
  ba8();
  ((Ba8 = typeof Ia6 == "object" && Ia6 && !Ia6.nodeType && Ia6),
    (ua8 = Ba8 && typeof ha6 == "object" && ha6 && !ha6.nodeType && ha6),
    (zFq = ua8 && ua8.exports === Ba8),
    (ma8 = zFq ? lH.Buffer : void 0),
    (wFq = ma8 ? ma8.isBuffer : void 0),
    (_Fq = wFq || xa8),
    (ib = _Fq));
});
function HFq(A, q) {
  var K = typeof A;
  return (
    (q = q == null ? $Fq : q),
    !!q &&
      (K == "number" || (K != "symbol" && OFq.test(A))) &&
      A > -1 &&
      A % 1 == 0 &&
      A < q
  );
}
var $Fq = 9007199254740991,
  OFq,
  cn;
var fk6 = E(() => {
  OFq = /^(?:0|[1-9]\d*)$/;
  cn = HFq;
});
function JFq(A) {
  return typeof A == "number" && A > -1 && A % 1 == 0 && A <= jFq;
}
var jFq = 9007199254740991,
  E_6;
var xa6 = E(() => {
  E_6 = JFq;
});
function mFq(A) {
  return oD(A) && E_6(A.length) && !!X$[Zv(A)];
}
var DFq = "[object Arguments]",
  XFq = "[object Array]",
  MFq = "[object Boolean]",
  PFq = "[object Date]",
  WFq = "[object Error]",
  GFq = "[object Function]",
  ZFq = "[object Map]",
  fFq = "[object Number]",
  TFq = "[object Object]",
  NFq = "[object RegExp]",
  VFq = "[object Set]",
  vFq = "[object String]",
  kFq = "[object WeakMap]",
  EFq = "[object ArrayBuffer]",
  LFq = "[object DataView]",
  yFq = "[object Float32Array]",
  RFq = "[object Float64Array]",
  CFq = "[object Int8Array]",
  SFq = "[object Int16Array]",
  hFq = "[object Int32Array]",
  IFq = "[object Uint8Array]",
  xFq = "[object Uint8ClampedArray]",
  bFq = "[object Uint16Array]",
  uFq = "[object Uint32Array]",
  X$,
  ga8;
var Fa8 = E(() => {
  NA6();
  xa6();
  lb();
  X$ = {};
  X$[yFq] =
    X$[RFq] =
    X$[CFq] =
    X$[SFq] =
    X$[hFq] =
    X$[IFq] =
    X$[xFq] =
    X$[bFq] =
    X$[uFq] =
      !0;
  X$[DFq] =
    X$[XFq] =
    X$[EFq] =
    X$[MFq] =
    X$[LFq] =
    X$[PFq] =
    X$[WFq] =
    X$[GFq] =
    X$[ZFq] =
    X$[fFq] =
    X$[TFq] =
    X$[NFq] =
    X$[VFq] =
    X$[vFq] =
    X$[kFq] =
      !1;
  ga8 = mFq;
});
function BFq(A) {
  return function (q) {
    return A(q);
  };
}
var L_6;
var ba6 = E(() => {
  L_6 = BFq;
});
var ma6 = {};
s1(ma6, { default: () => nb });
var pa8, Tk6, gFq, kh1, FFq, nb;
var Ba6 = E(() => {
  Jh1();
  ((pa8 = typeof ma6 == "object" && ma6 && !ma6.nodeType && ma6),
    (Tk6 = pa8 && typeof ua6 == "object" && ua6 && !ua6.nodeType && ua6),
    (gFq = Tk6 && Tk6.exports === pa8),
    (kh1 = gFq && Wa6.process),
    (FFq = (function () {
      try {
        var A = Tk6 && Tk6.require && Tk6.require("util").types;
        if (A) return A;
        return kh1 && kh1.binding && kh1.binding("util");
      } catch (q) {}
    })()),
    (nb = FFq));
});
var Qa8, pFq, y_6;
var ga6 = E(() => {
  Fa8();
  ba6();
  Ba6();
  ((Qa8 = nb && nb.isTypedArray), (pFq = Qa8 ? L_6(Qa8) : ga8), (y_6 = pFq));
});
function dFq(A, q) {
  var K = H2(A),
    Y = !K && op(A),
    z = !K && !Y && ib(A),
    w = !K && !Y && !z && y_6(A),
    _ = K || Y || z || w,
    $ = _ ? Ca8(A.length, String) : [],
    O = $.length;
  for (var H in A)
    if (
      (q || UFq.call(A, H)) &&
      !(
        _ &&
        (H == "length" ||
          (z && (H == "offset" || H == "parent")) ||
          (w && (H == "buffer" || H == "byteLength" || H == "byteOffset")) ||
          cn(H, O))
      )
    )
      $.push(H);
  return $;
}
var QFq, UFq, Fa6;
var Eh1 = E(() => {
  Sa8();
  Gk6();
  RZ();
  Zk6();
  fk6();
  ga6();
  ((QFq = Object.prototype), (UFq = QFq.hasOwnProperty));
  Fa6 = dFq;
});
function lFq(A) {
  var q = A && A.constructor,
    K = (typeof q == "function" && q.prototype) || cFq;
  return A === K;
}
var cFq, R_6;
var pa6 = E(() => {
  cFq = Object.prototype;
  R_6 = lFq;
});
function iFq(A, q) {
  return function (K) {
    return A(q(K));
  };
}
var Qa6;
var Lh1 = E(() => {
  Qa6 = iFq;
});
var nFq, Ua8;
var da8 = E(() => {
  Lh1();
  ((nFq = Qa6(Object.keys, Object)), (Ua8 = nFq));
});
function aFq(A) {
  if (!R_6(A)) return Ua8(A);
  var q = [];
  for (var K in Object(A)) if (oFq.call(A, K) && K != "constructor") q.push(K);
  return q;
}
var rFq, oFq, ca8;
var la8 = E(() => {
  pa6();
  da8();
  ((rFq = Object.prototype), (oFq = rFq.hasOwnProperty));
  ca8 = aFq;
});
function sFq(A) {
  return A != null && E_6(A.length) && !P_6(A);
}
var rb;
var C_6 = E(() => {
  Ga6();
  xa6();
  rb = sFq;
});
function tFq(A) {
  return rb(A) ? Fa6(A) : ca8(A);
}
var DL;
var vA6 = E(() => {
  Eh1();
  la8();
  C_6();
  DL = tFq;
});
function eFq(A) {
  return ya6(A, DL, k_6);
}
var Nk6;
var yh1 = E(() => {
  Th1();
  Sa6();
  vA6();
  Nk6 = eFq;
});
function Ypq(A, q, K, Y, z, w) {
  var _ = K & Apq,
    $ = Nk6(A),
    O = $.length,
    H = Nk6(q),
    j = H.length;
  if (O != j && !_) return !1;
  var J = O;
  while (J--) {
    var D = $[J];
    if (!(_ ? D in q : Kpq.call(q, D))) return !1;
  }
  var X = w.get(A),
    M = w.get(q);
  if (X && M) return X == q && M == A;
  var P = !0;
  (w.set(A, q), w.set(q, A));
  var W = _;
  while (++J < O) {
    D = $[J];
    var G = A[D],
      Z = q[D];
    if (Y) var f = _ ? Y(Z, G, D, q, A, w) : Y(G, Z, D, A, q, w);
    if (!(f === void 0 ? G === Z || z(G, Z, K, Y, w) : f)) {
      P = !1;
      break;
    }
    W || (W = D == "constructor");
  }
  if (P && !W) {
    var N = A.constructor,
      V = q.constructor;
    if (
      N != V &&
      "constructor" in A &&
      "constructor" in q &&
      !(
        typeof N == "function" &&
        N instanceof N &&
        typeof V == "function" &&
        V instanceof V
      )
    )
      P = !1;
  }
  return (w.delete(A), w.delete(q), P);
}
var Apq = 1,
  qpq,
  Kpq,
  ia8;
var na8 = E(() => {
  yh1();
  ((qpq = Object.prototype), (Kpq = qpq.hasOwnProperty));
  ia8 = Ypq;
});
var zpq, Ua6;
var ra8 = E(() => {
  gn();
  JL();
  ((zpq = uT(lH, "DataView")), (Ua6 = zpq));
});
var wpq, da6;
var oa8 = E(() => {
  gn();
  JL();
  ((wpq = uT(lH, "Promise")), (da6 = wpq));
});
var _pq, ln;
var Rh1 = E(() => {
  gn();
  JL();
  ((_pq = uT(lH, "Set")), (ln = _pq));
});
var $pq, ca6;
var aa8 = E(() => {
  gn();
  JL();
  (($pq = uT(lH, "WeakMap")), (ca6 = $pq));
});
var sa8 = "[object Map]",
  Opq = "[object Object]",
  ta8 = "[object Promise]",
  ea8 = "[object Set]",
  As8 = "[object WeakMap]",
  qs8 = "[object DataView]",
  Hpq,
  jpq,
  Jpq,
  Dpq,
  Xpq,
  kA6,
  ap;
var Vk6 = E(() => {
  ra8();
  fa6();
  oa8();
  Rh1();
  aa8();
  NA6();
  Dh1();
  ((Hpq = np(Ua6)),
    (jpq = np(Qn)),
    (Jpq = np(da6)),
    (Dpq = np(ln)),
    (Xpq = np(ca6)),
    (kA6 = Zv));
  if (
    (Ua6 && kA6(new Ua6(new ArrayBuffer(1))) != qs8) ||
    (Qn && kA6(new Qn()) != sa8) ||
    (da6 && kA6(da6.resolve()) != ta8) ||
    (ln && kA6(new ln()) != ea8) ||
    (ca6 && kA6(new ca6()) != As8)
  )
    kA6 = function (A) {
      var q = Zv(A),
        K = q == Opq ? A.constructor : void 0,
        Y = K ? np(K) : "";
      if (Y)
        switch (Y) {
          case Hpq:
            return qs8;
          case jpq:
            return sa8;
          case Jpq:
            return ta8;
          case Dpq:
            return ea8;
          case Xpq:
            return As8;
        }
      return q;
    };
  ap = kA6;
});
function Wpq(A, q, K, Y, z, w) {
  var _ = H2(A),
    $ = H2(q),
    O = _ ? Ys8 : ap(A),
    H = $ ? Ys8 : ap(q);
  ((O = O == Ks8 ? la6 : O), (H = H == Ks8 ? la6 : H));
  var j = O == la6,
    J = H == la6,
    D = O == H;
  if (D && ib(A)) {
    if (!ib(q)) return !1;
    ((_ = !0), (j = !1));
  }
  if (D && !j)
    return (
      w || (w = new cb()),
      _ || y_6(A) ? ka6(A, q, K, Y, z, w) : La8(A, q, O, K, Y, z, w)
    );
  if (!(K & Mpq)) {
    var X = j && zs8.call(A, "__wrapped__"),
      M = J && zs8.call(q, "__wrapped__");
    if (X || M) {
      var P = X ? A.value() : A,
        W = M ? q.value() : q;
      return (w || (w = new cb()), z(P, W, K, Y, w));
    }
  }
  if (!D) return !1;
  return (w || (w = new cb()), ia8(A, q, K, Y, z, w));
}
var Mpq = 1,
  Ks8 = "[object Arguments]",
  Ys8 = "[object Array]",
  la6 = "[object Object]",
  Ppq,
  zs8,
  ws8;
var _s8 = E(() => {
  Wk6();
  Gh1();
  ya8();
  na8();
  Vk6();
  RZ();
  Zk6();
  ga6();
  ((Ppq = Object.prototype), (zs8 = Ppq.hasOwnProperty));
  ws8 = Wpq;
});
function $s8(A, q, K, Y, z) {
  if (A === q) return !0;
  if (A == null || q == null || (!oD(A) && !oD(q))) return A !== A && q !== q;
  return ws8(A, q, K, Y, $s8, z);
}
var S_6;
var ia6 = E(() => {
  _s8();
  lb();
  S_6 = $s8;
});
function fpq(A, q, K, Y) {
  var z = K.length,
    w = z,
    _ = !Y;
  if (A == null) return !w;
  A = Object(A);
  while (z--) {
    var $ = K[z];
    if (_ && $[2] ? $[1] !== A[$[0]] : !($[0] in A)) return !1;
  }
  while (++z < w) {
    $ = K[z];
    var O = $[0],
      H = A[O],
      j = $[1];
    if (_ && $[2]) {
      if (H === void 0 && !(O in A)) return !1;
    } else {
      var J = new cb();
      if (Y) var D = Y(H, j, O, A, q, J);
      if (!(D === void 0 ? S_6(j, H, Gpq | Zpq, Y, J) : D)) return !1;
    }
  }
  return !0;
}
var Gpq = 1,
  Zpq = 2,
  Os8;
var Hs8 = E(() => {
  Wk6();
  ia6();
  Os8 = fpq;
});
function Tpq(A) {
  return A === A && !O2(A);
}
var na6;
var Ch1 = E(() => {
  yZ();
  na6 = Tpq;
});
function Npq(A) {
  var q = DL(A),
    K = q.length;
  while (K--) {
    var Y = q[K],
      z = A[Y];
    q[K] = [Y, z, na6(z)];
  }
  return q;
}
var js8;
var Js8 = E(() => {
  Ch1();
  vA6();
  js8 = Npq;
});
function Vpq(A, q) {
  return function (K) {
    if (K == null) return !1;
    return K[A] === q && (q !== void 0 || A in Object(K));
  };
}
var ra6;
var Sh1 = E(() => {
  ra6 = Vpq;
});
function vpq(A) {
  var q = js8(A);
  if (q.length == 1 && q[0][2]) return ra6(q[0][0], q[0][1]);
  return function (K) {
    return K === A || Os8(K, A, q);
  };
}
var Ds8;
var Xs8 = E(() => {
  Hs8();
  Js8();
  Sh1();
  Ds8 = vpq;
});
function Epq(A) {
  return typeof A == "symbol" || (oD(A) && Zv(A) == kpq);
}
var kpq = "[object Symbol]",
  nn;
var vk6 = E(() => {
  NA6();
  lb();
  nn = Epq;
});
function Rpq(A, q) {
  if (H2(A)) return !1;
  var K = typeof A;
  if (K == "number" || K == "symbol" || K == "boolean" || A == null || nn(A))
    return !0;
  return ypq.test(A) || !Lpq.test(A) || (q != null && A in Object(q));
}
var Lpq, ypq, h_6;
var oa6 = E(() => {
  RZ();
  vk6();
  ((Lpq = /\.|\[(?:[^[\]]*|(["'])(?:(?!\1)[^\\]|\\.)*?\1)\]/), (ypq = /^\w*$/));
  h_6 = Rpq;
});
function Spq(A) {
  var q = T8(A, function (Y) {
      if (K.size === Cpq) K.clear();
      return Y;
    }),
    K = q.cache;
  return q;
}
var Cpq = 500,
  Ms8;
var Ps8 = E(() => {
  Sq();
  Ms8 = Spq;
});
var hpq, Ipq, xpq, Ws8;
var Gs8 = E(() => {
  Ps8();
  ((hpq =
    /[^.[\]]+|\[(?:(-?\d+(?:\.\d+)?)|(["'])((?:(?!\2)[^\\]|\\.)*?)\2)\]|(?=(?:\.|\[\])(?:\.|\[\]|$))/g),
    (Ipq = /\\(\\)?/g),
    (xpq = Ms8(function (A) {
      var q = [];
      if (A.charCodeAt(0) === 46) q.push("");
      return (
        A.replace(hpq, function (K, Y, z, w) {
          q.push(z ? w.replace(Ipq, "$1") : Y || K);
        }),
        q
      );
    })),
    (Ws8 = xpq));
});
function bpq(A, q) {
  var K = -1,
    Y = A == null ? 0 : A.length,
    z = Array(Y);
  while (++K < Y) z[K] = q(A[K], K, A);
  return z;
}
var I_6;
var aa6 = E(() => {
  I_6 = bpq;
});
function Ts8(A) {
  if (typeof A == "string") return A;
  if (H2(A)) return I_6(A, Ts8) + "";
  if (nn(A)) return fs8 ? fs8.call(A) : "";
  var q = A + "";
  return q == "0" && 1 / A == -upq ? "-0" : q;
}
var upq = 1 / 0,
  Zs8,
  fs8,
  Ns8;
var Vs8 = E(() => {
  TA6();
  aa6();
  RZ();
  vk6();
  ((Zs8 = aX ? aX.prototype : void 0), (fs8 = Zs8 ? Zs8.toString : void 0));
  Ns8 = Ts8;
});
function mpq(A) {
  return A == null ? "" : Ns8(A);
}
var x_6;
var sa6 = E(() => {
  Vs8();
  x_6 = mpq;
});
function Bpq(A, q) {
  if (H2(A)) return A;
  return h_6(A, q) ? [A] : Ws8(x_6(A));
}
var ob;
var b_6 = E(() => {
  RZ();
  oa6();
  Gs8();
  sa6();
  ob = Bpq;
});
function Fpq(A) {
  if (typeof A == "string" || nn(A)) return A;
  var q = A + "";
  return q == "0" && 1 / A == -gpq ? "-0" : q;
}
var gpq = 1 / 0,
  XL;
var EA6 = E(() => {
  vk6();
  XL = Fpq;
});
function ppq(A, q) {
  q = ob(q, A);
  var K = 0,
    Y = q.length;
  while (A != null && K < Y) A = A[XL(q[K++])];
  return K && K == Y ? A : void 0;
}
var u_6;
var ta6 = E(() => {
  b_6();
  EA6();
  u_6 = ppq;
});
function Qpq(A, q, K) {
  var Y = A == null ? void 0 : u_6(A, q);
  return Y === void 0 ? K : Y;
}
var vs8;
var ks8 = E(() => {
  ta6();
  vs8 = Qpq;
});
function Upq(A, q) {
  return A != null && q in Object(A);
}
var Es8;
var Ls8 = E(() => {
  Es8 = Upq;
});
function dpq(A, q, K) {
  q = ob(q, A);
  var Y = -1,
    z = q.length,
    w = !1;
  while (++Y < z) {
    var _ = XL(q[Y]);
    if (!(w = A != null && K(A, _))) break;
    A = A[_];
  }
  if (w || ++Y != z) return w;
  return (
    (z = A == null ? 0 : A.length),
    !!z && E_6(z) && cn(_, z) && (H2(A) || op(A))
  );
}
var ys8;
var Rs8 = E(() => {
  b_6();
  Gk6();
  RZ();
  fk6();
  xa6();
  EA6();
  ys8 = dpq;
});
function cpq(A, q) {
  return A != null && ys8(A, q, Es8);
}
var Cs8;
var Ss8 = E(() => {
  Ls8();
  Rs8();
  Cs8 = cpq;
});
function npq(A, q) {
  if (h_6(A) && na6(q)) return ra6(XL(A), q);
  return function (K) {
    var Y = vs8(K, A);
    return Y === void 0 && Y === q ? Cs8(K, A) : S_6(q, Y, lpq | ipq);
  };
}
var lpq = 1,
  ipq = 2,
  hs8;
var Is8 = E(() => {
  ia6();
  ks8();
  Ss8();
  oa6();
  Ch1();
  Sh1();
  EA6();
  hs8 = npq;
});
function rpq(A) {
  return A;
}
var m_6;
var ea6 = E(() => {
  m_6 = rpq;
});
function opq(A) {
  return function (q) {
    return q == null ? void 0 : q[A];
  };
}
var xs8;
var bs8 = E(() => {
  xs8 = opq;
});
function apq(A) {
  return function (q) {
    return u_6(q, A);
  };
}
var us8;
var ms8 = E(() => {
  ta6();
  us8 = apq;
});
function spq(A) {
  return h_6(A) ? xs8(XL(A)) : us8(A);
}
var Bs8;
var gs8 = E(() => {
  bs8();
  ms8();
  oa6();
  EA6();
  Bs8 = spq;
});
function tpq(A) {
  if (typeof A == "function") return A;
  if (A == null) return m_6;
  if (typeof A == "object") return H2(A) ? hs8(A[0], A[1]) : Ds8(A);
  return Bs8(A);
}
var ab;
var B_6 = E(() => {
  Xs8();
  Is8();
  ea6();
  RZ();
  gs8();
  ab = tpq;
});
function epq(A, q) {
  var K,
    Y = -1,
    z = A.length;
  while (++Y < z) {
    var w = q(A[Y]);
    if (w !== void 0) K = K === void 0 ? w : K + w;
  }
  return K;
}
var Fs8;
var ps8 = E(() => {
  Fs8 = epq;
});
function AQq(A, q) {
  return A && A.length ? Fs8(A, ab(q, 2)) : 0;
}
var g_6;
var Qs8 = E(() => {
  B_6();
  ps8();
  g_6 = AQq;
});
function Us8() {
  return hh1;
}
function ds8(A) {
  hh1 = A;
}
function M$() {
  hh1 = null;
}
function cs8() {
  return Ih1;
}
function ls8(A) {
  Ih1 = A;
}
function is8() {
  Ih1 = void 0;
}
var hh1 = null,
  Ih1;
var UI1 = {};
s1(UI1, {
  updateLastInteractionTime: () => yA6,
  setUseCoworkPlugins: () => Nv,
  setTracerProvider: () => Os6,
  setTeleportedSessionInfo: () => Bk6,
  setSystemPromptSectionCacheEntry: () => mI1,
  setStatsStore: () => lh1,
  setSessionTrustAccepted: () => uk6,
  setSessionSource: () => jI1,
  setSessionPersistenceDisabled: () => vI1,
  setSessionIngressToken: () => hA6,
  setSessionId: () => Z0,
  setSessionBypassPermissionsMode: () => VI1,
  setSdkBetas: () => AI1,
  setResumedTranscriptPath: () => kk6,
  setPromptId: () => Qk6,
  setPromptCache1hAllowlist: () => pI1,
  setOriginalCwd: () => LA6,
  setOauthTokenFromFd: () => IA6,
  setNeedsPlanModeExitAttachment: () => tb,
  setModelStrings: () => Rk6,
  setMeterProvider: () => $s6,
  setMeter: () => qI1,
  setMainThreadAgentType: () => tp,
  setMainLoopModelOverride: () => LW,
  setLspRecommendationShownThisSession: () => yI1,
  setLoggerProvider: () => ws6,
  setLastEmittedDate: () => n_6,
  setLastAPIRequest: () => WI1,
  setIsRemoteMode: () => xI1,
  setIsInteractive: () => OI1,
  setIsInWorktree: () => Fk6,
  setInlinePlugins: () => TI1,
  setInitialMainLoopModel: () => eh1,
  setInitJsonSchema: () => RI1,
  setHasUnknownModelCost: () => Ys6,
  setHasExitedPlanMode: () => PL,
  setFlagSettingsPath: () => JI1,
  setFlagSettingsInline: () => DI1,
  setEventLogger: () => _s6,
  setDirectConnectServerUrl: () => KQq,
  setCwdState: () => As6,
  setCostStateForRestore: () => yk6,
  setClientType: () => HI1,
  setChromeFlagOverride: () => NI1,
  setApiKeyFromFd: () => xA6,
  setAllowedSettingSources: () => fI1,
  setAdditionalDirectoriesForClaudeMd: () => pk6,
  resetTurnToolDuration: () => dh1,
  resetTurnHookDuration: () => Uh1,
  resetTurnClassifierDuration: () => ch1,
  resetTotalDurationStateAndCost_FOR_TESTS_ONLY: () => YQq,
  resetStateForTests: () => ts8,
  resetSdkInitState: () => es8,
  resetModelStringsForTestingOnly: () => JQq,
  resetCostState: () => U_6,
  registerHookCallbacks: () => mA6,
  regenerateSessionId: () => xh1,
  preferThirdPartyAuthentication: () => Ik6,
  needsPlanModeExitAttachment: () => EI1,
  markFirstTeleportMessageLogged: () => Ds6,
  isSkillTriggerActive: () => GQq,
  isSessionPersistenceDisabled: () => ML,
  hasUnknownModelCost: () => sh1,
  hasShownLspRecommendationThisSession: () => LI1,
  hasExitedPlanModeInSession: () => kI1,
  handlePlanModeTransition: () => sp,
  getUseCoworkPlugins: () => bk6,
  getUsageForModel: () => th1,
  getTurnToolDurationMs: () => _Qq,
  getTurnToolCount: () => $Qq,
  getTurnHookDurationMs: () => zQq,
  getTurnHookCount: () => wQq,
  getTurnClassifierDurationMs: () => OQq,
  getTurnClassifierCount: () => jQq,
  getTracerProvider: () => SA6,
  getTotalWebSearchRequests: () => ah1,
  getTotalToolDuration: () => ph1,
  getTotalOutputTokens: () => Lk6,
  getTotalLinesRemoved: () => CA6,
  getTotalLinesAdded: () => RA6,
  getTotalInputTokens: () => Ek6,
  getTotalDuration: () => F_6,
  getTotalCostUSD: () => sX,
  getTotalCacheReadInputTokens: () => rh1,
  getTotalCacheCreationInputTokens: () => oh1,
  getTotalAPIDurationWithoutRetries: () => Fh1,
  getTotalAPIDuration: () => fv,
  getTokenCounter: () => c_6,
  getTeleportedSessionInfo: () => Js6,
  getSystemPromptSectionCache: () => uI1,
  getStatsStore: () => p_6,
  getSlowOperations: () => Kt8,
  getSessionTrustAccepted: () => i_6,
  getSessionSource: () => XQq,
  getSessionIngressToken: () => XI1,
  getSessionId: () => getSessionId,
  getSessionCounter: () => KI1,
  getSessionBypassPermissionsMode: () => uA6,
  getSdkBetas: () => iH,
  getResumedTranscriptPath: () => uh1,
  getRegisteredHooks: () => mk6,
  getPromptId: () => QI1,
  getPromptCacheBreaks: () => zt8,
  getPromptCache1hAllowlist: () => FI1,
  getProjectRoot: () => pw,
  getPrCounter: () => Ck6,
  getPlanSlugCache: () => BA6,
  getParentSessionId: () => bh1,
  getOriginalCwd: () => HA,
  getOauthTokenFromFd: () => MI1,
  getModelUsage: () => VS,
  getModelStrings: () => d_6,
  getMeterProvider: () => $I1,
  getMeter: () => DQq,
  getMainThreadAgentType: () => pA6,
  getMainLoopModelOverride: () => vS,
  getLoggerProvider: () => hk6,
  getLocCounter: () => zs6,
  getLastInteractionTime: () => sb,
  getLastEmittedDate: () => gI1,
  getLastAPIRequest: () => GI1,
  getIsRemoteMode: () => Eq,
  getIsNonInteractiveSession: () => C7,
  getIsInteractive: () => Tv,
  getIsInWorktree: () => bI1,
  getInvokedSkillsForAgent: () => Xs6,
  getInvokedSkills: () => WQq,
  getInlinePlugins: () => bA6,
  getInitialMainLoopModel: () => Q_6,
  getInitJsonSchema: () => js6,
  getFlagSettingsPath: () => on,
  getFlagSettingsInline: () => l_6,
  getEventLogger: () => _I1,
  getDirectConnectServerUrl: () => mh1,
  getCwdState: () => NS,
  getCostCounter: () => zI1,
  getCommitCounter: () => YI1,
  getCodeEditToolDecisionCounter: () => Sk6,
  getClientType: () => rn,
  getChromeFlagOverride: () => xk6,
  getApiKeyFromFd: () => PI1,
  getAllowedSettingSources: () => ZI1,
  getAgentColorMap: () => Hs6,
  getAdditionalDirectoriesForClaudeMd: () => mT,
  getActiveTimeCounter: () => wI1,
  flushInteractionTime: () => nh1,
  clearSystemPromptSectionState: () => BI1,
  clearRegisteredPluginHooks: () => CI1,
  clearRegisteredHooks: () => PQq,
  clearPromptCacheBreaks: () => II1,
  clearInvokedSkillsForAgent: () => FA6,
  clearInvokedSkills: () => SI1,
  clearActivatedSkillTriggers: () => hI1,
  addToTurnHookDuration: () => Qh1,
  addToTurnClassifierDuration: () => HQq,
  addToTotalLinesChanged: () => Ks6,
  addToTotalDurationState: () => Bh1,
  addToTotalCostState: () => gh1,
  addToToolDuration: () => qs6,
  addToInMemoryErrorLog: () => MQq,
  addSlowOperation: () => qt8,
  addPromptCacheBreak: () => Yt8,
  addInvokedSkill: () => gA6,
  activateSkillTrigger: () => gk6,
});
import { cwd as qQq } from "process";
import { realpathSync as ns8 } from "fs";
import { randomUUID as os8 } from "crypto";
function as8() {
  let A = "";
  if (
    typeof process < "u" &&
    typeof process.cwd === "function" &&
    typeof ns8 === "function"
  )
    A = ns8(qQq()).normalize("NFC");
  return {
    originalCwd: A,
    projectRoot: A,
    totalCostUSD: 0,
    totalAPIDuration: 0,
    totalAPIDurationWithoutRetries: 0,
    totalToolDuration: 0,
    turnHookDurationMs: 0,
    turnToolDurationMs: 0,
    turnClassifierDurationMs: 0,
    turnToolCount: 0,
    turnHookCount: 0,
    turnClassifierCount: 0,
    startTime: Date.now(),
    lastInteractionTime: Date.now(),
    totalLinesAdded: 0,
    totalLinesRemoved: 0,
    hasUnknownModelCost: !1,
    cwd: A,
    modelUsage: {},
    mainLoopModelOverride: void 0,
    initialMainLoopModel: null,
    modelStrings: null,
    isInteractive: !1,
    clientType: "cli",
    sessionSource: void 0,
    sessionIngressToken: void 0,
    oauthTokenFromFd: void 0,
    apiKeyFromFd: void 0,
    flagSettingsPath: void 0,
    flagSettingsInline: null,
    allowedSettingSources: [
      "userSettings",
      "projectSettings",
      "localSettings",
      "flagSettings",
      "policySettings",
    ],
    meter: null,
    sessionCounter: null,
    locCounter: null,
    prCounter: null,
    commitCounter: null,
    costCounter: null,
    tokenCounter: null,
    codeEditToolDecisionCounter: null,
    activeTimeCounter: null,
    statsStore: null,
    sessionId: os8(),
    parentSessionId: void 0,
    loggerProvider: null,
    eventLogger: null,
    meterProvider: null,
    tracerProvider: null,
    agentColorMap: new Map(),
    agentColorIndex: 0,
    lastAPIRequest: null,
    inMemoryErrorLog: [],
    inlinePlugins: [],
    chromeFlagOverride: void 0,
    useCoworkPlugins: !1,
    sessionBypassPermissionsMode: !1,
    sessionTrustAccepted: !1,
    sessionPersistenceDisabled: !1,
    hasExitedPlanMode: !1,
    needsPlanModeExitAttachment: !1,
    lspRecommendationShownThisSession: !1,
    initJsonSchema: null,
    registeredHooks: null,
    planSlugCache: new Map(),
    teleportedSessionInfo: null,
    invokedSkills: new Map(),
    activatedSkillTriggers: new Set(),
    slowOperations: [],
    promptCacheBreaks: [],
    sdkBetas: void 0,
    mainThreadAgentType: void 0,
    isRemoteMode: !1,
    isInWorktree: !1,
    directConnectServerUrl: void 0,
    systemPromptSectionCache: new Map(),
    lastEmittedDate: null,
    additionalDirectoriesForClaudeMd: [],
    resumedTranscriptPath: null,
    promptCache1hAllowlist: null,
    promptId: null,
  };
}
function getSessionId() {
  return x1.sessionId;
}
function xh1(A = {}) {
  if (A.setCurrentAsParent) x1.parentSessionId = x1.sessionId;
  return (
    (x1.sessionId = os8()),
    (x1.resumedTranscriptPath = null),
    x1.sessionId
  );
}
function bh1() {
  return x1.parentSessionId;
}
function Z0(A) {
  x1.sessionId = A;
}
function HA() {
  return x1.originalCwd;
}
function pw() {
  return x1.projectRoot;
}
function LA6(A) {
  x1.originalCwd = A.normalize("NFC");
}
function uh1() {
  return x1.resumedTranscriptPath;
}
function kk6(A) {
  x1.resumedTranscriptPath = A;
}
function NS() {
  return x1.cwd;
}
function As6(A) {
  x1.cwd = A.normalize("NFC");
}
function mh1() {
  return x1.directConnectServerUrl;
}
function KQq(A) {
  x1.directConnectServerUrl = A;
}
function Bh1(A, q) {
  ((x1.totalAPIDuration += A), (x1.totalAPIDurationWithoutRetries += q));
}
function YQq() {
  ((x1.totalAPIDuration = 0),
    (x1.totalAPIDurationWithoutRetries = 0),
    (x1.totalCostUSD = 0));
}
function gh1(A, q, K) {
  ((x1.modelUsage[K] = q), (x1.totalCostUSD += A));
}
function sX() {
  return x1.totalCostUSD;
}
function fv() {
  return x1.totalAPIDuration;
}
function F_6() {
  return Date.now() - x1.startTime;
}
function Fh1() {
  return x1.totalAPIDurationWithoutRetries;
}
function ph1() {
  return x1.totalToolDuration;
}
function qs6(A) {
  ((x1.totalToolDuration += A),
    (x1.turnToolDurationMs += A),
    x1.turnToolCount++);
}
function zQq() {
  return x1.turnHookDurationMs;
}
function Qh1(A) {
  ((x1.turnHookDurationMs += A), x1.turnHookCount++);
}
function Uh1() {
  ((x1.turnHookDurationMs = 0), (x1.turnHookCount = 0));
}
function wQq() {
  return x1.turnHookCount;
}
function _Qq() {
  return x1.turnToolDurationMs;
}
function dh1() {
  ((x1.turnToolDurationMs = 0), (x1.turnToolCount = 0));
}
function $Qq() {
  return x1.turnToolCount;
}
function OQq() {
  return x1.turnClassifierDurationMs;
}
function HQq(A) {
  ((x1.turnClassifierDurationMs += A), x1.turnClassifierCount++);
}
function ch1() {
  ((x1.turnClassifierDurationMs = 0), (x1.turnClassifierCount = 0));
}
function jQq() {
  return x1.turnClassifierCount;
}
function p_6() {
  return x1.statsStore;
}
function lh1(A) {
  x1.statsStore = A;
}
function yA6(A) {
  if (A) ss8();
  else ih1 = !0;
}
function nh1() {
  if (ih1) ss8();
}
function ss8() {
  ((x1.lastInteractionTime = Date.now()), (ih1 = !1));
}
function Ks6(A, q) {
  ((x1.totalLinesAdded += A), (x1.totalLinesRemoved += q));
}
function RA6() {
  return x1.totalLinesAdded;
}
function CA6() {
  return x1.totalLinesRemoved;
}
function Ek6() {
  return g_6(Object.values(x1.modelUsage), "inputTokens");
}
function Lk6() {
  return g_6(Object.values(x1.modelUsage), "outputTokens");
}
function rh1() {
  return g_6(Object.values(x1.modelUsage), "cacheReadInputTokens");
}
function oh1() {
  return g_6(Object.values(x1.modelUsage), "cacheCreationInputTokens");
}
function ah1() {
  return g_6(Object.values(x1.modelUsage), "webSearchRequests");
}
function Ys6() {
  x1.hasUnknownModelCost = !0;
}
function sh1() {
  return x1.hasUnknownModelCost;
}
function sb() {
  return x1.lastInteractionTime;
}
function VS() {
  return x1.modelUsage;
}
function th1(A) {
  return x1.modelUsage[A];
}
function vS() {
  return x1.mainLoopModelOverride;
}
function Q_6() {
  return x1.initialMainLoopModel;
}
function LW(A) {
  x1.mainLoopModelOverride = A;
}
function eh1(A) {
  x1.initialMainLoopModel = A;
}
function iH() {
  return x1.sdkBetas;
}
function AI1(A) {
  x1.sdkBetas = A;
}
function U_6() {
  ((x1.totalCostUSD = 0),
    (x1.totalAPIDuration = 0),
    (x1.totalAPIDurationWithoutRetries = 0),
    (x1.totalToolDuration = 0),
    (x1.startTime = Date.now()),
    (x1.totalLinesAdded = 0),
    (x1.totalLinesRemoved = 0),
    (x1.hasUnknownModelCost = !1),
    (x1.modelUsage = {}),
    (x1.promptId = null));
}
function yk6({
  totalCostUSD: A,
  totalAPIDuration: q,
  totalAPIDurationWithoutRetries: K,
  totalToolDuration: Y,
  totalLinesAdded: z,
  totalLinesRemoved: w,
  lastDuration: _,
  modelUsage: $,
}) {
  if (
    ((x1.totalCostUSD = A),
    (x1.totalAPIDuration = q),
    (x1.totalAPIDurationWithoutRetries = K),
    (x1.totalToolDuration = Y),
    (x1.totalLinesAdded = z),
    (x1.totalLinesRemoved = w),
    $)
  )
    x1.modelUsage = $;
  if (_) x1.startTime = Date.now() - _;
}
function ts8() {
  throw Error("resetStateForTests can only be called in tests");
}
function d_6() {
  return x1.modelStrings;
}
function Rk6(A) {
  x1.modelStrings = A;
}
function JQq() {
  x1.modelStrings = null;
}
function qI1(A, q) {
  ((x1.meter = A),
    (x1.sessionCounter = q("claude_code.session.count", {
      description: "Count of CLI sessions started",
    })),
    (x1.locCounter = q("claude_code.lines_of_code.count", {
      description:
        "Count of lines of code modified, with the 'type' attribute indicating whether lines were added or removed",
    })),
    (x1.prCounter = q("claude_code.pull_request.count", {
      description: "Number of pull requests created",
    })),
    (x1.commitCounter = q("claude_code.commit.count", {
      description: "Number of git commits created",
    })),
    (x1.costCounter = q("claude_code.cost.usage", {
      description: "Cost of the Claude Code session",
      unit: "USD",
    })),
    (x1.tokenCounter = q("claude_code.token.usage", {
      description: "Number of tokens used",
      unit: "tokens",
    })),
    (x1.codeEditToolDecisionCounter = q("claude_code.code_edit_tool.decision", {
      description:
        "Count of code editing tool permission decisions (accept/reject) for Edit, Write, and NotebookEdit tools",
    })),
    (x1.activeTimeCounter = q("claude_code.active_time.total", {
      description: "Total active time in seconds",
      unit: "s",
    })));
}
function DQq() {
  return x1.meter;
}
function KI1() {
  return x1.sessionCounter;
}
function zs6() {
  return x1.locCounter;
}
function Ck6() {
  return x1.prCounter;
}
function YI1() {
  return x1.commitCounter;
}
function zI1() {
  return x1.costCounter;
}
function c_6() {
  return x1.tokenCounter;
}
function Sk6() {
  return x1.codeEditToolDecisionCounter;
}
function wI1() {
  return x1.activeTimeCounter;
}
function hk6() {
  return x1.loggerProvider;
}
function ws6(A) {
  x1.loggerProvider = A;
}
function _I1() {
  return x1.eventLogger;
}
function _s6(A) {
  x1.eventLogger = A;
}
function $I1() {
  return x1.meterProvider;
}
function $s6(A) {
  x1.meterProvider = A;
}
function SA6() {
  return x1.tracerProvider;
}
function Os6(A) {
  x1.tracerProvider = A;
}
function C7() {
  return !x1.isInteractive;
}
function Tv() {
  return x1.isInteractive;
}
function OI1(A) {
  x1.isInteractive = A;
}
function rn() {
  return x1.clientType;
}
function HI1(A) {
  x1.clientType = A;
}
function XQq() {
  return x1.sessionSource;
}
function jI1(A) {
  x1.sessionSource = A;
}
function Hs6() {
  return x1.agentColorMap;
}
function on() {
  return x1.flagSettingsPath;
}
function JI1(A) {
  x1.flagSettingsPath = A;
}
function l_6() {
  return x1.flagSettingsInline;
}
function DI1(A) {
  x1.flagSettingsInline = A;
}
function XI1() {
  return x1.sessionIngressToken;
}
function hA6(A) {
  x1.sessionIngressToken = A;
}
function MI1() {
  return x1.oauthTokenFromFd;
}
function IA6(A) {
  x1.oauthTokenFromFd = A;
}
function PI1() {
  return x1.apiKeyFromFd;
}
function xA6(A) {
  x1.apiKeyFromFd = A;
}
function WI1(A) {
  x1.lastAPIRequest = A;
}
function GI1() {
  return x1.lastAPIRequest;
}
function MQq(A) {
  if (x1.inMemoryErrorLog.length >= 100) x1.inMemoryErrorLog.shift();
  x1.inMemoryErrorLog.push(A);
}
function ZI1() {
  return x1.allowedSettingSources;
}
function fI1(A) {
  x1.allowedSettingSources = A;
}
function Ik6() {
  return C7() && x1.clientType !== "claude-vscode";
}
function TI1(A) {
  x1.inlinePlugins = A;
}
function bA6() {
  return x1.inlinePlugins;
}
function NI1(A) {
  x1.chromeFlagOverride = A;
}
function xk6() {
  return x1.chromeFlagOverride;
}
function Nv(A) {
  ((x1.useCoworkPlugins = A), M$());
}
function bk6() {
  return x1.useCoworkPlugins;
}
function VI1(A) {
  x1.sessionBypassPermissionsMode = A;
}
function uA6() {
  return x1.sessionBypassPermissionsMode;
}
function uk6(A) {
  x1.sessionTrustAccepted = A;
}
function i_6() {
  return x1.sessionTrustAccepted;
}
function vI1(A) {
  x1.sessionPersistenceDisabled = A;
}
function ML() {
  return x1.sessionPersistenceDisabled;
}
function kI1() {
  return x1.hasExitedPlanMode;
}
function PL(A) {
  x1.hasExitedPlanMode = A;
}
function EI1() {
  return x1.needsPlanModeExitAttachment;
}
function tb(A) {
  x1.needsPlanModeExitAttachment = A;
}
function sp(A, q) {
  if (q === "plan" && A !== "plan") x1.needsPlanModeExitAttachment = !1;
  if (A === "plan" && q !== "plan") x1.needsPlanModeExitAttachment = !0;
}
function LI1() {
  return x1.lspRecommendationShownThisSession;
}
function yI1(A) {
  x1.lspRecommendationShownThisSession = A;
}
function RI1(A) {
  x1.initJsonSchema = A;
}
function js6() {
  return x1.initJsonSchema;
}
function mA6(A) {
  if (!x1.registeredHooks) x1.registeredHooks = {};
  for (let [q, K] of Object.entries(A)) {
    let Y = q;
    if (!x1.registeredHooks[Y]) x1.registeredHooks[Y] = [];
    x1.registeredHooks[Y].push(...K);
  }
}
function mk6() {
  return x1.registeredHooks;
}
function PQq() {
  x1.registeredHooks = null;
}
function CI1() {
  if (!x1.registeredHooks) return;
  let A = {};
  for (let [q, K] of Object.entries(x1.registeredHooks)) {
    let Y = K.filter((z) => !("pluginRoot" in z));
    if (Y.length > 0) A[q] = Y;
  }
  x1.registeredHooks = Object.keys(A).length > 0 ? A : null;
}
function es8() {
  ((x1.initJsonSchema = null), (x1.registeredHooks = null));
}
function BA6() {
  return x1.planSlugCache;
}
function Bk6(A) {
  x1.teleportedSessionInfo = {
    isTeleported: !0,
    hasLoggedFirstMessage: !1,
    sessionId: A.sessionId,
  };
}
function Js6() {
  return x1.teleportedSessionInfo;
}
function Ds6() {
  if (x1.teleportedSessionInfo)
    x1.teleportedSessionInfo.hasLoggedFirstMessage = !0;
}
function gA6(A, q, K, Y = null) {
  let z = `${Y ?? ""}:${A}`;
  x1.invokedSkills.set(z, {
    skillName: A,
    skillPath: q,
    content: K,
    invokedAt: Date.now(),
    agentId: Y,
  });
}
function WQq() {
  return x1.invokedSkills;
}
function Xs6(A) {
  let q = A ?? null,
    K = new Map();
  for (let [Y, z] of x1.invokedSkills) if (z.agentId === q) K.set(Y, z);
  return K;
}
function SI1() {
  x1.invokedSkills.clear();
}
function FA6(A) {
  for (let [q, K] of x1.invokedSkills)
    if (K.agentId === A) x1.invokedSkills.delete(q);
}
function gk6(A) {
  x1.activatedSkillTriggers.add(A);
}
function GQq(A) {
  return x1.activatedSkillTriggers.has(A);
}
function hI1() {
  x1.activatedSkillTriggers.clear();
}
function qt8(A, q) {
  return;
}
function Kt8() {
  let A = Date.now();
  return (
    (x1.slowOperations = x1.slowOperations.filter(
      (q) => A - q.timestamp < At8,
    )),
    [...x1.slowOperations]
  );
}
function Yt8(A, q) {
  return;
}
function zt8() {
  let A = Date.now();
  return (
    (x1.promptCacheBreaks = x1.promptCacheBreaks.filter(
      (q) => A - q.timestamp < ZQq,
    )),
    [...x1.promptCacheBreaks]
  );
}
function II1() {
  x1.promptCacheBreaks = [];
}
function pA6() {
  return x1.mainThreadAgentType;
}
function tp(A) {
  x1.mainThreadAgentType = A;
}
function Eq() {
  return x1.isRemoteMode;
}
function xI1(A) {
  x1.isRemoteMode = A;
}
function bI1() {
  return x1.isInWorktree;
}
function Fk6(A) {
  x1.isInWorktree = A;
}
function uI1() {
  return x1.systemPromptSectionCache;
}
function mI1(A, q) {
  x1.systemPromptSectionCache.set(A, q);
}
function BI1() {
  x1.systemPromptSectionCache.clear();
}
function gI1() {
  return x1.lastEmittedDate;
}
function n_6(A) {
  x1.lastEmittedDate = A;
}
function mT() {
  return x1.additionalDirectoriesForClaudeMd;
}
function pk6(A) {
  x1.additionalDirectoriesForClaudeMd = A;
}
function FI1() {
  return x1.promptCache1hAllowlist;
}
function pI1(A) {
  x1.promptCache1hAllowlist = A;
}
function QI1() {
  return x1.promptId;
}
function Qk6(A) {
  x1.promptId = A;
}
var x1,
  ih1 = !1,
  rs8 = 10,
  At8 = 1e4,
  ZQq = 20000;
var B1 = E(() => {
  Qs8();
  x1 = as8();
});
function fQq(A, q) {
  var K = -1,
    Y = A == null ? 0 : A.length;
  while (++K < Y) if (q(A[K], K, A) === !1) break;
  return A;
}
var wt8;
var _t8 = E(() => {
  wt8 = fQq;
});
var TQq, r_6;
var dI1 = E(() => {
  gn();
  ((TQq = (function () {
    try {
      var A = uT(Object, "defineProperty");
      return (A({}, "", {}), A);
    } catch (q) {}
  })()),
    (r_6 = TQq));
});
function NQq(A, q, K) {
  if (q == "__proto__" && r_6)
    r_6(A, q, { configurable: !0, enumerable: !0, value: K, writable: !0 });
  else A[q] = K;
}
var an;
var Uk6 = E(() => {
  dI1();
  an = NQq;
});
function kQq(A, q, K) {
  var Y = A[q];
  if (!(vQq.call(A, q) && db(Y, K)) || (K === void 0 && !(q in A))) an(A, q, K);
}
var VQq, vQq, sn;
var dk6 = E(() => {
  Uk6();
  G_6();
  ((VQq = Object.prototype), (vQq = VQq.hasOwnProperty));
  sn = kQq;
});
function EQq(A, q, K, Y) {
  var z = !K;
  K || (K = {});
  var w = -1,
    _ = q.length;
  while (++w < _) {
    var $ = q[w],
      O = Y ? Y(K[$], A[$], $, K, A) : void 0;
    if (O === void 0) O = A[$];
    if (z) an(K, $, O);
    else sn(K, $, O);
  }
  return K;
}
var WL;
var QA6 = E(() => {
  dk6();
  Uk6();
  WL = EQq;
});
function LQq(A, q) {
  return A && WL(q, DL(q), A);
}
var $t8;
var Ot8 = E(() => {
  QA6();
  vA6();
  $t8 = LQq;
});
function yQq(A) {
  var q = [];
  if (A != null) for (var K in Object(A)) q.push(K);
  return q;
}
var Ht8;
var jt8 = E(() => {
  Ht8 = yQq;
});
function SQq(A) {
  if (!O2(A)) return Ht8(A);
  var q = R_6(A),
    K = [];
  for (var Y in A)
    if (!(Y == "constructor" && (q || !CQq.call(A, Y)))) K.push(Y);
  return K;
}
var RQq, CQq, Jt8;
var Dt8 = E(() => {
  yZ();
  pa6();
  jt8();
  ((RQq = Object.prototype), (CQq = RQq.hasOwnProperty));
  Jt8 = SQq;
});
function hQq(A) {
  return rb(A) ? Fa6(A, !0) : Jt8(A);
}
var eb;
var o_6 = E(() => {
  Eh1();
  Dt8();
  C_6();
  eb = hQq;
});
function IQq(A, q) {
  return A && WL(q, eb(q), A);
}
var Xt8;
var Mt8 = E(() => {
  QA6();
  o_6();
  Xt8 = IQq;
});
var Ps6 = {};
s1(Ps6, { default: () => ck6 });
function bQq(A, q) {
  if (q) return A.slice();
  var K = A.length,
    Y = Gt8 ? Gt8(K) : new A.constructor(K);
  return (A.copy(Y), Y);
}
var Zt8, Pt8, xQq, Wt8, Gt8, ck6;
var cI1 = E(() => {
  JL();
  ((Zt8 = typeof Ps6 == "object" && Ps6 && !Ps6.nodeType && Ps6),
    (Pt8 = Zt8 && typeof Ms6 == "object" && Ms6 && !Ms6.nodeType && Ms6),
    (xQq = Pt8 && Pt8.exports === Zt8),
    (Wt8 = xQq ? lH.Buffer : void 0),
    (Gt8 = Wt8 ? Wt8.allocUnsafe : void 0));
  ck6 = bQq;
});
function uQq(A, q) {
  var K = -1,
    Y = A.length;
  q || (q = Array(Y));
  while (++K < Y) q[K] = A[K];
  return q;
}
var Ws6;
var lI1 = E(() => {
  Ws6 = uQq;
});
function mQq(A, q) {
  return WL(A, k_6(A), q);
}
var ft8;
var Tt8 = E(() => {
  QA6();
  Sa6();
  ft8 = mQq;
});
var BQq, a_6;
var Gs6 = E(() => {
  Lh1();
  ((BQq = Qa6(Object.getPrototypeOf, Object)), (a_6 = BQq));
});
var gQq, FQq, Zs6;
var iI1 = E(() => {
  La6();
  Gs6();
  Sa6();
  Vh1();
  ((gQq = Object.getOwnPropertySymbols),
    (FQq = !gQq
      ? Ca6
      : function (A) {
          var q = [];
          while (A) (v_6(q, k_6(A)), (A = a_6(A)));
          return q;
        }),
    (Zs6 = FQq));
});
function pQq(A, q) {
  return WL(A, Zs6(A), q);
}
var Nt8;
var Vt8 = E(() => {
  QA6();
  iI1();
  Nt8 = pQq;
});
function QQq(A) {
  return ya6(A, eb, Zs6);
}
var fs6;
var nI1 = E(() => {
  Th1();
  iI1();
  o_6();
  fs6 = QQq;
});
function cQq(A) {
  var q = A.length,
    K = new A.constructor(q);
  if (q && typeof A[0] == "string" && dQq.call(A, "index"))
    ((K.index = A.index), (K.input = A.input));
  return K;
}
var UQq, dQq, vt8;
var kt8 = E(() => {
  ((UQq = Object.prototype), (dQq = UQq.hasOwnProperty));
  vt8 = cQq;
});
function lQq(A) {
  var q = new A.constructor(A.byteLength);
  return (new N_6(q).set(new N_6(A)), q);
}
var s_6;
var Ts6 = E(() => {
  Zh1();
  s_6 = lQq;
});
function iQq(A, q) {
  var K = q ? s_6(A.buffer) : A.buffer;
  return new A.constructor(K, A.byteOffset, A.byteLength);
}
var Et8;
var Lt8 = E(() => {
  Ts6();
  Et8 = iQq;
});
function rQq(A) {
  var q = new A.constructor(A.source, nQq.exec(A));
  return ((q.lastIndex = A.lastIndex), q);
}
var nQq, yt8;
var Rt8 = E(() => {
  nQq = /\w*$/;
  yt8 = rQq;
});
function oQq(A) {
  return St8 ? Object(St8.call(A)) : {};
}
var Ct8, St8, ht8;
var It8 = E(() => {
  TA6();
  ((Ct8 = aX ? aX.prototype : void 0), (St8 = Ct8 ? Ct8.valueOf : void 0));
  ht8 = oQq;
});
function aQq(A, q) {
  var K = q ? s_6(A.buffer) : A.buffer;
  return new A.constructor(K, A.byteOffset, A.length);
}
var Ns6;
var rI1 = E(() => {
  Ts6();
  Ns6 = aQq;
});
function WUq(A, q, K) {
  var Y = A.constructor;
  switch (q) {
    case wUq:
      return s_6(A);
    case sQq:
    case tQq:
      return new Y(+A);
    case _Uq:
      return Et8(A, K);
    case $Uq:
    case OUq:
    case HUq:
    case jUq:
    case JUq:
    case DUq:
    case XUq:
    case MUq:
    case PUq:
      return Ns6(A, K);
    case eQq:
      return new Y();
    case AUq:
    case YUq:
      return new Y(A);
    case qUq:
      return yt8(A);
    case KUq:
      return new Y();
    case zUq:
      return ht8(A);
  }
}
var sQq = "[object Boolean]",
  tQq = "[object Date]",
  eQq = "[object Map]",
  AUq = "[object Number]",
  qUq = "[object RegExp]",
  KUq = "[object Set]",
  YUq = "[object String]",
  zUq = "[object Symbol]",
  wUq = "[object ArrayBuffer]",
  _Uq = "[object DataView]",
  $Uq = "[object Float32Array]",
  OUq = "[object Float64Array]",
  HUq = "[object Int8Array]",
  jUq = "[object Int16Array]",
  JUq = "[object Int32Array]",
  DUq = "[object Uint8Array]",
  XUq = "[object Uint8ClampedArray]",
  MUq = "[object Uint16Array]",
  PUq = "[object Uint32Array]",
  xt8;
var bt8 = E(() => {
  Ts6();
  Lt8();
  Rt8();
  It8();
  rI1();
  xt8 = WUq;
});
var ut8, GUq, mt8;
var Bt8 = E(() => {
  yZ();
  ((ut8 = Object.create),
    (GUq = (function () {
      function A() {}
      return function (q) {
        if (!O2(q)) return {};
        if (ut8) return ut8(q);
        A.prototype = q;
        var K = new A();
        return ((A.prototype = void 0), K);
      };
    })()),
    (mt8 = GUq));
});
function ZUq(A) {
  return typeof A.constructor == "function" && !R_6(A) ? mt8(a_6(A)) : {};
}
var Vs6;
var oI1 = E(() => {
  Bt8();
  Gs6();
  pa6();
  Vs6 = ZUq;
});
function TUq(A) {
  return oD(A) && ap(A) == fUq;
}
var fUq = "[object Map]",
  gt8;
var Ft8 = E(() => {
  Vk6();
  lb();
  gt8 = TUq;
});
var pt8, NUq, Qt8;
var Ut8 = E(() => {
  Ft8();
  ba6();
  Ba6();
  ((pt8 = nb && nb.isMap), (NUq = pt8 ? L_6(pt8) : gt8), (Qt8 = NUq));
});
function vUq(A) {
  return oD(A) && ap(A) == VUq;
}
var VUq = "[object Set]",
  dt8;
var ct8 = E(() => {
  Vk6();
  lb();
  dt8 = vUq;
});
var lt8, kUq, it8;
var nt8 = E(() => {
  ct8();
  ba6();
  Ba6();
  ((lt8 = nb && nb.isSet), (kUq = lt8 ? L_6(lt8) : dt8), (it8 = kUq));
});
function vs6(A, q, K, Y, z, w) {
  var _,
    $ = q & EUq,
    O = q & LUq,
    H = q & yUq;
  if (K) _ = z ? K(A, Y, z, w) : K(A);
  if (_ !== void 0) return _;
  if (!O2(A)) return A;
  var j = H2(A);
  if (j) {
    if (((_ = vt8(A)), !$)) return Ws6(A, _);
  } else {
    var J = ap(A),
      D = J == ot8 || J == IUq;
    if (ib(A)) return ck6(A, $);
    if (J == at8 || J == rt8 || (D && !z)) {
      if (((_ = O || D ? {} : Vs6(A)), !$))
        return O ? Nt8(A, Xt8(_, A)) : ft8(A, $t8(_, A));
    } else {
      if (!E_[J]) return z ? A : {};
      _ = xt8(A, J, $);
    }
  }
  w || (w = new cb());
  var X = w.get(A);
  if (X) return X;
  if ((w.set(A, _), it8(A)))
    A.forEach(function (W) {
      _.add(vs6(W, q, K, W, A, w));
    });
  else if (Qt8(A))
    A.forEach(function (W, G) {
      _.set(G, vs6(W, q, K, G, A, w));
    });
  var M = H ? (O ? fs6 : Nk6) : O ? eb : DL,
    P = j ? void 0 : M(A);
  return (
    wt8(P || A, function (W, G) {
      if (P) ((G = W), (W = A[G]));
      sn(_, G, vs6(W, q, K, G, A, w));
    }),
    _
  );
}
var EUq = 1,
  LUq = 2,
  yUq = 4,
  rt8 = "[object Arguments]",
  RUq = "[object Array]",
  CUq = "[object Boolean]",
  SUq = "[object Date]",
  hUq = "[object Error]",
  ot8 = "[object Function]",
  IUq = "[object GeneratorFunction]",
  xUq = "[object Map]",
  bUq = "[object Number]",
  at8 = "[object Object]",
  uUq = "[object RegExp]",
  mUq = "[object Set]",
  BUq = "[object String]",
  gUq = "[object Symbol]",
  FUq = "[object WeakMap]",
  pUq = "[object ArrayBuffer]",
  QUq = "[object DataView]",
  UUq = "[object Float32Array]",
  dUq = "[object Float64Array]",
  cUq = "[object Int8Array]",
  lUq = "[object Int16Array]",
  iUq = "[object Int32Array]",
  nUq = "[object Uint8Array]",
  rUq = "[object Uint8ClampedArray]",
  oUq = "[object Uint16Array]",
  aUq = "[object Uint32Array]",
  E_,
  ks6;
var aI1 = E(() => {
  Wk6();
  _t8();
  dk6();
  Ot8();
  Mt8();
  cI1();
  lI1();
  Tt8();
  Vt8();
  yh1();
  nI1();
  Vk6();
  kt8();
  bt8();
  oI1();
  RZ();
  Zk6();
  Ut8();
  yZ();
  nt8();
  vA6();
  o_6();
  E_ = {};
  E_[rt8] =
    E_[RUq] =
    E_[pUq] =
    E_[QUq] =
    E_[CUq] =
    E_[SUq] =
    E_[UUq] =
    E_[dUq] =
    E_[cUq] =
    E_[lUq] =
    E_[iUq] =
    E_[xUq] =
    E_[bUq] =
    E_[at8] =
    E_[uUq] =
    E_[mUq] =
    E_[BUq] =
    E_[gUq] =
    E_[nUq] =
    E_[rUq] =
    E_[oUq] =
    E_[aUq] =
      !0;
  E_[hUq] = E_[ot8] = E_[FUq] = !1;
  ks6 = vs6;
});
function eUq(A) {
  return ks6(A, sUq | tUq);
}
var sUq = 1,
  tUq = 4,
  st8;
var tt8 = E(() => {
  aI1();
  st8 = eUq;
});
import {
  writeFileSync as et8,
  openSync as Adq,
  fsyncSync as qdq,
  closeSync as Kdq,
} from "fs";
function zdq() {
  return Ydq;
}
function trySafeStringify(A, q, K) {
  let z = [];
  try {
    const Y = hY(z, m2`JSON.stringify(${A})`, 0);
    return JSON.stringify(A, q, K);
  } catch (w) {
    var _ = w,
      $ = 1;
  } finally {
    IY(z, _, $);
  }
}
function t_6(A) {
  let K = [];
  try {
    const q = hY(K, m2`cloneDeep(${A})`, 0);
    return st8(A);
  } catch (Y) {
    var z = Y,
      w = 1;
  } finally {
    IY(K, z, w);
  }
}
function Nz(A, q, K) {
  let w = [];
  try {
    const Y = hY(w, m2`fs.writeFileSync(${A}, ${q})`, 0);
    let z =
      K !== null && typeof K === "object" && "flush" in K && K.flush === !0;
    if (z) {
      let H = typeof K === "object" && "encoding" in K ? K.encoding : void 0,
        j = typeof K === "object" && "mode" in K ? K.mode : void 0,
        J;
      try {
        ((J = Adq(A, "w", j)), et8(J, q, { encoding: H ?? void 0 }), qdq(J));
      } finally {
        if (J !== void 0) Kdq(J);
      }
    } else et8(A, q, K);
  } catch (_) {
    var $ = _,
      O = 1;
  } finally {
    IY(w, $, O);
  }
}
var NBz,
  Ydq,
  m2,
  w8 = (A, q) => {
    let Y = [];
    try {
      const K = hY(Y, m2`JSON.parse(${A})`, 0);
      return typeof q > "u" ? JSON.parse(A) : JSON.parse(A, q);
    } catch (z) {
      var w = z,
        _ = 1;
    } finally {
      IY(Y, w, _);
    }
  };
var r1 = E(() => {
  f1();
  B1();
  tt8();
  ((NBz = (() => {
    let A = process.env.CLAUDE_CODE_SLOW_OPERATION_THRESHOLD_MS;
    if (A !== void 0) {
      let q = Number(A);
      if (!Number.isNaN(q) && q >= 0) return q;
    }
    return 1 / 0;
  })()),
    (Ydq = { [Symbol.dispose]() {} }));
  m2 = zdq;
});
import * as J3 from "fs";
import { homedir as Ae8 } from "os";
import * as tn from "path";
import {
  stat as wdq,
  readdir as _dq,
  readFile as qe8,
  unlink as $dq,
  rmdir as Odq,
  rm as Hdq,
  mkdir as jdq,
  rename as Jdq,
  open as Es6,
} from "fs/promises";
function P$(A, q) {
  if (q.startsWith("//") || q.startsWith("\\\\"))
    return { resolvedPath: q, isSymlink: !1 };
  if (!A.existsSync(q)) return { resolvedPath: q, isSymlink: !1 };
  try {
    let K = A.lstatSync(q);
    if (
      K.isFIFO() ||
      K.isSocket() ||
      K.isCharacterDevice() ||
      K.isBlockDevice()
    )
      return { resolvedPath: q, isSymlink: !1 };
    let Y = A.realpathSync(q);
    return { resolvedPath: Y, isSymlink: Y !== q };
  } catch (K) {
    return { resolvedPath: q, isSymlink: !1 };
  }
}
function Au(A, q, K) {
  let { resolvedPath: Y } = P$(A, q);
  if (K.has(Y)) return !0;
  return (K.add(Y), !1);
}
function UA6(A) {
  let q = A;
  if (q === "~") q = Ae8().normalize("NFC");
  else if (q.startsWith("~/")) q = tn.join(Ae8().normalize("NFC"), q.slice(2));
  let K = new Set(),
    Y = P1();
  if ((K.add(q), q.startsWith("//") || q.startsWith("\\\\")))
    return Array.from(K);
  try {
    let _ = q,
      $ = new Set(),
      O = 40;
    for (let H = 0; H < O; H++) {
      if ($.has(_)) break;
      if (($.add(_), !Y.existsSync(_))) break;
      let j = Y.lstatSync(_);
      if (
        j.isFIFO() ||
        j.isSocket() ||
        j.isCharacterDevice() ||
        j.isBlockDevice()
      )
        break;
      if (!j.isSymbolicLink()) break;
      let J = Y.readlinkSync(_),
        D = tn.isAbsolute(J) ? J : tn.resolve(tn.dirname(_), J);
      (K.add(D), (_ = D));
    }
  } catch {}
  let { resolvedPath: z, isSymlink: w } = P$(Y, q);
  if (w && z !== q) K.add(z);
  return Array.from(K);
}
function P1() {
  return Xdq;
}
async function Ls6(A, q, K) {
  let O = [];
  try {
    const Y = hY(O, await Es6(A, "r"), 1);
    let z = (await Y.stat()).size;
    if (z <= q) return null;
    let w = Math.min(z - q, K);
    let _ = Buffer.allocUnsafe(w);
    let $ = 0;
    while ($ < w) {
      let { bytesRead: X } = await Y.read(_, $, w - $, q + $);
      if (X === 0) break;
      $ += X;
    }
    return { content: _.toString("utf8", 0, $), bytesRead: $, bytesTotal: z };
  } catch (H) {
    var j = H,
      J = 1;
  } finally {
    var D = IY(O, j, J);
    D && (await D);
  }
}
async function e_6(A, q) {
  let O = [];
  try {
    const K = hY(O, await Es6(A, "r"), 1);
    let Y = (await K.stat()).size;
    if (Y === 0) return { content: "", bytesRead: 0, bytesTotal: 0 };
    let z = Math.max(0, Y - q);
    let w = Y - z;
    let _ = Buffer.allocUnsafe(w);
    let $ = 0;
    while ($ < w) {
      let { bytesRead: X } = await K.read(_, $, w - $, z + $);
      if (X === 0) break;
      $ += X;
    }
    return { content: _.toString("utf8", 0, $), bytesRead: $, bytesTotal: Y };
  } catch (H) {
    var j = H,
      J = 1;
  } finally {
    var D = IY(O, j, J);
    D && (await D);
  }
}
async function* Ke8(A) {
  let K = await Es6(A, "r");
  try {
    let z = (await K.stat()).size,
      w = "",
      _ = Buffer.alloc(4096);
    while (z > 0) {
      let $ = Math.min(4096, z);
      ((z -= $), await K.read(_, 0, $, z));
      let H = (_.toString("utf8", 0, $) + w).split(`
`);
      w = H[0] || "";
      for (let j = H.length - 1; j >= 1; j--) {
        let J = H[j];
        if (J) yield J;
      }
    }
    if (w) yield w;
  } finally {
    await K.close();
  }
}
var Ddq, Xdq;
var $7 = E(() => {
  r1();
  ((Ddq = {
    cwd() {
      return process.cwd();
    },
    existsSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.existsSync(${A})`, 0);
        return J3.existsSync(A);
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    async stat(A) {
      return wdq(A);
    },
    async readdir(A) {
      return _dq(A, { withFileTypes: !0 });
    },
    async unlink(A) {
      return $dq(A);
    },
    async rmdir(A) {
      return Odq(A);
    },
    async rm(A, q) {
      return Hdq(A, q);
    },
    async mkdir(A, q) {
      await jdq(A, { recursive: !0, ...q });
    },
    async readFile(A, q) {
      return qe8(A, { encoding: q.encoding });
    },
    async rename(A, q) {
      return Jdq(A, q);
    },
    statSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.statSync(${A})`, 0);
        return J3.statSync(A);
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    lstatSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.lstatSync(${A})`, 0);
        return J3.lstatSync(A);
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    readFileSync(A, q) {
      let Y = [];
      try {
        const K = hY(Y, m2`fs.readFileSync(${A})`, 0);
        return J3.readFileSync(A, { encoding: q.encoding });
      } catch (z) {
        var w = z,
          _ = 1;
      } finally {
        IY(Y, w, _);
      }
    },
    readFileBytesSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.readFileBytesSync(${A})`, 0);
        return J3.readFileSync(A);
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    readSync(A, q) {
      let z = [];
      try {
        const K = hY(z, m2`fs.readSync(${A}, ${q.length} bytes)`, 0);
        let Y = void 0;
        try {
          Y = J3.openSync(A, "r");
          let O = Buffer.alloc(q.length),
            H = J3.readSync(Y, O, 0, q.length, 0);
          return { buffer: O, bytesRead: H };
        } finally {
          if (Y) J3.closeSync(Y);
        }
      } catch (w) {
        var _ = w,
          $ = 1;
      } finally {
        IY(z, _, $);
      }
    },
    appendFileSync(A, q, K) {
      let z = [];
      try {
        const Y = hY(z, m2`fs.appendFileSync(${A}, ${q.length} chars)`, 0);
        if (!J3.existsSync(A) && K?.mode !== void 0) {
          let O = J3.openSync(A, "a", K.mode);
          try {
            J3.appendFileSync(O, q);
          } finally {
            J3.closeSync(O);
          }
        } else J3.appendFileSync(A, q);
      } catch (w) {
        var _ = w,
          $ = 1;
      } finally {
        IY(z, _, $);
      }
    },
    copyFileSync(A, q) {
      let Y = [];
      try {
        const K = hY(Y, m2`fs.copyFileSync(${A} → ${q})`, 0);
        J3.copyFileSync(A, q);
      } catch (z) {
        var w = z,
          _ = 1;
      } finally {
        IY(Y, w, _);
      }
    },
    unlinkSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.unlinkSync(${A})`, 0);
        J3.unlinkSync(A);
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    renameSync(A, q) {
      let Y = [];
      try {
        const K = hY(Y, m2`fs.renameSync(${A} → ${q})`, 0);
        J3.renameSync(A, q);
      } catch (z) {
        var w = z,
          _ = 1;
      } finally {
        IY(Y, w, _);
      }
    },
    linkSync(A, q) {
      let Y = [];
      try {
        const K = hY(Y, m2`fs.linkSync(${A} → ${q})`, 0);
        J3.linkSync(A, q);
      } catch (z) {
        var w = z,
          _ = 1;
      } finally {
        IY(Y, w, _);
      }
    },
    symlinkSync(A, q, K) {
      let z = [];
      try {
        const Y = hY(z, m2`fs.symlinkSync(${A} → ${q})`, 0);
        J3.symlinkSync(A, q, K);
      } catch (w) {
        var _ = w,
          $ = 1;
      } finally {
        IY(z, _, $);
      }
    },
    readlinkSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.readlinkSync(${A})`, 0);
        return J3.readlinkSync(A);
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    realpathSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.realpathSync(${A})`, 0);
        return J3.realpathSync(A).normalize("NFC");
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    mkdirSync(A, q) {
      let Y = [];
      try {
        const K = hY(Y, m2`fs.mkdirSync(${A})`, 0);
        if (!J3.existsSync(A)) {
          let $ = { recursive: !0 };
          if (q?.mode !== void 0) $.mode = q.mode;
          J3.mkdirSync(A, $);
        }
      } catch (z) {
        var w = z,
          _ = 1;
      } finally {
        IY(Y, w, _);
      }
    },
    readdirSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.readdirSync(${A})`, 0);
        return J3.readdirSync(A, { withFileTypes: !0 });
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    readdirStringSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.readdirStringSync(${A})`, 0);
        return J3.readdirSync(A);
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    isDirEmptySync(A) {
      let Y = [];
      try {
        const q = hY(Y, m2`fs.isDirEmptySync(${A})`, 0);
        let K = this.readdirSync(A);
        return K.length === 0;
      } catch (z) {
        var w = z,
          _ = 1;
      } finally {
        IY(Y, w, _);
      }
    },
    rmdirSync(A) {
      let K = [];
      try {
        const q = hY(K, m2`fs.rmdirSync(${A})`, 0);
        J3.rmdirSync(A);
      } catch (Y) {
        var z = Y,
          w = 1;
      } finally {
        IY(K, z, w);
      }
    },
    rmSync(A, q) {
      let Y = [];
      try {
        const K = hY(Y, m2`fs.rmSync(${A})`, 0);
        J3.rmSync(A, q);
      } catch (z) {
        var w = z,
          _ = 1;
      } finally {
        IY(Y, w, _);
      }
    },
    createWriteStream(A) {
      return J3.createWriteStream(A);
    },
    async readFileBytes(A, q) {
      if (q === void 0) return qe8(A);
      let K = await Es6(A, "r");
      try {
        let { size: Y } = await K.stat(),
          z = Math.min(Y, q),
          w = Buffer.allocUnsafe(z),
          _ = 0;
        while (_ < z) {
          let { bytesRead: $ } = await K.read(w, _, z - _, _);
          if ($ === 0) break;
          _ += $;
        }
        return _ < z ? w.subarray(0, _) : w;
      } finally {
        await K.close();
      }
    },
  }),
    (Xdq = Ddq));
});
import { join as Ye8 } from "path";
import { homedir as Mdq } from "os";
function _A() {
  return (process.env.CLAUDE_CONFIG_DIR ?? Ye8(Mdq(), ".claude")).normalize(
    "NFC",
  );
}
function CZ() {
  return Ye8(_A(), "teams");
}
function sI1(A) {
  let q = process.env.NODE_OPTIONS;
  if (!q) return !1;
  return q.split(/\s+/).includes(A);
}
function isTruthy(A) {
  if (!A) return !1;
  if (typeof A === "boolean") return A;
  let q = A.toLowerCase().trim();
  return ["1", "true", "yes", "on"].includes(q);
}
function Qw(A) {
  if (A === void 0) return !1;
  if (typeof A === "boolean") return !A;
  if (!A) return !1;
  let q = A.toLowerCase().trim();
  return ["0", "false", "no", "off"].includes(q);
}
function ze8(A) {
  let q = {};
  if (A)
    for (let K of A) {
      let [Y, ...z] = K.split("=");
      if (!Y || z.length === 0)
        throw Error(
          `Invalid environment variable format: ${K}, environment variables should be added as: -e KEY1=value1 -e KEY2=value2`,
        );
      q[Y] = z.join("=");
    }
  return q;
}
function dA6() {
  return (
    process.env.AWS_REGION || process.env.AWS_DEFAULT_REGION || "us-east-1"
  );
}
function ys6() {
  return process.env.CLOUD_ML_REGION || "us-east5";
}
function tI1() {
  return isTruthy(process.env.CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR);
}
function SZ() {
  return !1;
}
function Rs6(A) {
  if (A) {
    let q = Pdq.find(([K]) => A.startsWith(K));
    if (q) return process.env[q[1]] || ys6();
  }
  return ys6();
}
var Pdq;
var N8 = E(() => {
  Pdq = [
    ["claude-haiku-4-5", "VERTEX_REGION_CLAUDE_HAIKU_4_5"],
    ["claude-3-5-haiku", "VERTEX_REGION_CLAUDE_3_5_HAIKU"],
    ["claude-3-5-sonnet", "VERTEX_REGION_CLAUDE_3_5_SONNET"],
    ["claude-3-7-sonnet", "VERTEX_REGION_CLAUDE_3_7_SONNET"],
    ["claude-opus-4-1", "VERTEX_REGION_CLAUDE_4_1_OPUS"],
    ["claude-opus-4", "VERTEX_REGION_CLAUDE_4_0_OPUS"],
    ["claude-sonnet-4-6", "VERTEX_REGION_CLAUDE_4_6_SONNET"],
    ["claude-sonnet-4-5", "VERTEX_REGION_CLAUDE_4_5_SONNET"],
    ["claude-sonnet-4", "VERTEX_REGION_CLAUDE_4_0_SONNET"],
  ];
});
function A$6({
  writeFn: A,
  flushIntervalMs: q = 1000,
  maxBufferSize: K = 100,
  maxBufferBytes: Y = 1 / 0,
  immediateMode: z = !1,
}) {
  let w = [],
    _ = 0,
    $ = null;
  function O() {
    if ($) (clearTimeout($), ($ = null));
  }
  function H() {
    if (w.length === 0) return;
    (A(w.join("")), (w = []), (_ = 0), O());
  }
  function j() {
    if (!$) $ = setTimeout(H, q);
  }
  return {
    write(J) {
      if (z) {
        A(J);
        return;
      }
      if ((w.push(J), (_ += J.length), j(), w.length >= K || _ >= Y)) H();
    },
    flush: H,
    dispose() {
      H();
    },
  };
}
function Xq(A) {
  return (eI1.add(A), () => eI1.delete(A));
}
async function we8() {
  await Promise.all(Array.from(eI1).map((A) => A()));
}
var eI1;
var Vz = E(() => {
  eI1 = new Set();
});
import { dirname as _e8, join as $e8 } from "path";
function Gdq(A) {
  if (
    typeof process > "u" ||
    typeof process.versions > "u" ||
    typeof process.versions.node > "u"
  )
    return !1;
  let q = Wdq();
  return wa8(A, q);
}
function je8(A) {
  He8 = A;
}
function Zdq() {
  if (!Cs6) {
    let A = null;
    ((Cs6 = A$6({
      writeFn: (q) => {
        let K = cA6(),
          Y = _e8(K);
        if (A !== Y) {
          try {
            P1().mkdirSync(Y);
          } catch {}
          A = Y;
        }
        (P1().appendFileSync(K, q), fdq());
      },
      flushIntervalMs: 1000,
      maxBufferSize: 100,
      immediateMode: en(),
    })),
      Xq(async () => Cs6?.dispose()));
  }
  return Cs6;
}
function writeDebugLog(A, { level: q } = { level: "debug" }) {
  if (!Gdq(A)) return;
  if (
    He8 &&
    A.includes(`
`)
  )
    A = trySafeStringify(A);
  let Y = `${new Date().toISOString()} [${q.toUpperCase()}] ${A.trim()}
`;
  if (qu()) {
    dn(Y);
    return;
  }
  Zdq().write(Y);
}
function cA6() {
  return (
    Oe8() ??
    process.env.CLAUDE_CODE_DEBUG_LOGS_DIR ??
    $e8(_A(), "debug", `${getSessionId()}.txt`)
  );
}
function GL(A, q) {
  return;
}
var en,
  Wdq,
  qu,
  Oe8,
  He8 = !1,
  Cs6 = null,
  fdq;
var f1 = E(() => {
  Sq();
  _a8();
  $7();
  N8();
  B1();
  Vz();
  r1();
  ((en = T8(() => {
    return (
      isTruthy(process.env.DEBUG) ||
      isTruthy(process.env.DEBUG_SDK) ||
      process.argv.includes("--debug") ||
      process.argv.includes("-d") ||
      qu() ||
      process.argv.some((A) => A.startsWith("--debug=")) ||
      Oe8() !== null
    );
  })),
    (Wdq = T8(() => {
      let A = process.argv.find((K) => K.startsWith("--debug="));
      if (!A) return null;
      let q = A.substring(8);
      return za8(q);
    })),
    (qu = T8(() => {
      return (
        process.argv.includes("--debug-to-stderr") ||
        process.argv.includes("-d2e")
      );
    })),
    (Oe8 = T8(() => {
      for (let A = 0; A < process.argv.length; A++) {
        let q = process.argv[A];
        if (q.startsWith("--debug-file=")) return q.substring(13);
        if (q === "--debug-file" && A + 1 < process.argv.length)
          return process.argv[A + 1];
      }
      return null;
    })));
  fdq = T8(() => {
    if (process.argv[2] === "--ripgrep") return;
    try {
      let A = cA6(),
        q = _e8(A),
        K = $e8(q, "latest");
      try {
        P1().mkdirSync(q);
      } catch {}
      try {
        P1().unlinkSync(K);
      } catch {}
      P1().symlinkSync(A, K);
    } catch {}
  });
});
function Je8(A) {
  if (q$6 !== null)
    throw Error(
      "Analytics sink already attached - cannot attach more than once",
    );
  if (((q$6 = A), Ss6.length > 0)) {
    let q = [...Ss6];
    ((Ss6.length = 0),
      queueMicrotask(() => {
        for (let K of q)
          if (K.async) q$6.logEventAsync(K.eventName, K.metadata);
          else q$6.logEvent(K.eventName, K.metadata);
      }));
  }
}
function emitEvent(A, q) {
  if (q$6 === null) {
    Ss6.push({ eventName: A, metadata: q, async: !1 });
    return;
  }
  q$6.logEvent(A, q);
}
var Ss6,
  q$6 = null;
var u1 = E(() => {
  Ss6 = [];
});
var Te8 = {};
s1(Te8, {
  profileReport: () => ik6,
  profileCheckpoint: () => Bq,
  logStartupPerf: () => fe8,
  isDetailedProfilingEnabled: () => kdq,
  getStartupPerfLogPath: () => Ze8,
});
import { join as Tdq, dirname as Ndq } from "path";
function Kx1() {
  if (!Ax1) Ax1 = require("perf_hooks").performance;
  return Ax1;
}
function Bq(A) {
  if (!We8) return;
  if ((Kx1().mark(A), lk6)) Ge8.set(A, process.memoryUsage());
}
function qx1(A) {
  return A.toFixed(3);
}
function De8(A) {
  return (A / 1024 / 1024).toFixed(2);
}
function Xe8() {
  if (!lk6) return "Startup profiling not enabled";
  let q = Kx1().getEntriesByType("mark");
  if (q.length === 0) return "No profiling checkpoints recorded";
  let K = [];
  (K.push("=".repeat(80)),
    K.push("STARTUP PROFILING REPORT"),
    K.push("=".repeat(80)),
    K.push(""));
  let Y = 0;
  for (let _ of q) {
    let $ = qx1(_.startTime),
      O = qx1(_.startTime - Y),
      H = Ge8.get(_.name),
      j = H ? ` | RSS: ${De8(H.rss)}MB, Heap: ${De8(H.heapUsed)}MB` : "";
    (K.push(`[+${$.padStart(8)}ms] (+${O.padStart(7)}ms) ${_.name}${j}`),
      (Y = _.startTime));
  }
  let z = q[q.length - 1],
    w = qx1(z?.startTime ?? 0);
  return (
    K.push(""),
    K.push(`Total startup time: ${w}ms`),
    K.push("=".repeat(80)),
    K.join(`
`)
  );
}
function ik6() {
  if (Me8) return;
  if (((Me8 = !0), fe8(), lk6)) {
    let A = Ze8(),
      q = Ndq(A);
    (P1().mkdirSync(q),
      Nz(A, Xe8(), { encoding: "utf8", flush: !0 }),
      writeDebugLog("Startup profiling report:"),
      writeDebugLog(Xe8()));
  }
}
function kdq() {
  return lk6;
}
function Ze8() {
  return Tdq(_A(), "startup-perf", `${getSessionId()}.txt`);
}
function fe8() {
  if (!Pe8) return;
  let q = Kx1().getEntriesByType("mark");
  if (q.length === 0) return;
  let K = new Map();
  for (let z of q) K.set(z.name, z.startTime);
  let Y = {};
  for (let [z, [w, _]] of Object.entries(vdq)) {
    let $ = K.get(w),
      O = K.get(_);
    if ($ !== void 0 && O !== void 0) Y[`${z}_ms`] = Math.round(O - $);
  }
  ((Y.checkpoint_count = q.length), emitEvent("tengu_startup_perf", Y));
}
var lk6,
  Vdq = 0.005,
  Pe8,
  We8,
  Ge8,
  Ax1 = null,
  vdq,
  Me8 = !1;
var kS = E(() => {
  f1();
  u1();
  N8();
  B1();
  $7();
  r1();
  ((lk6 = process.env.CLAUDE_CODE_PROFILE_STARTUP === "1"),
    (Pe8 = Math.random() < Vdq),
    (We8 = lk6 || Pe8),
    (Ge8 = new Map()));
  vdq = {
    import_time: ["cli_entry", "main_tsx_imports_loaded"],
    init_time: ["init_function_start", "init_function_end"],
    settings_time: ["eagerLoadSettings_start", "eagerLoadSettings_end"],
    total_time: ["cli_entry", "main_after_run"],
  };
  if (We8) Bq("profiler_initialized");
});
var Ne8 = {};
s1(Ne8, { ripgrepMain: () => Sdq });
import { createRequire as Edq } from "module";
import { fileURLToPath as Ldq } from "url";
import { dirname as ydq, join as Rdq } from "path";
import { spawnSync as Cdq } from "child_process";
function Sdq(A) {
  if (process.env.RIPGREP_EMBEDDED === "true")
    return (
      Cdq(process.execPath, ["--no-config", ...A], {
        argv0: "rg",
        stdio: "inherit",
      }).status ?? 1
    );
  let q;
  if (process.env.RIPGREP_NODE_PATH)
    q = require(process.env.RIPGREP_NODE_PATH).ripgrepMain;
  else {
    let K = Rdq(ydq(Ldq(import.meta.url)), "ripgrep.node");
    q = Edq(import.meta.url)(K).ripgrepMain;
  }
  return q(["--no-config", ...A]);
}
var Ve8 = () => {};
function f8(A, q, K) {
  function Y($, O) {
    var H;
    (Object.defineProperty($, "_zod", { value: $._zod ?? {}, enumerable: !1 }),
      (H = $._zod).traits ?? (H.traits = new Set()),
      $._zod.traits.add(A),
      q($, O));
    for (let j in _.prototype)
      if (!(j in $))
        Object.defineProperty($, j, { value: _.prototype[j].bind($) });
    (($._zod.constr = _), ($._zod.def = O));
  }
  let z = K?.Parent ?? Object;
  class w extends z {}
  Object.defineProperty(w, "name", { value: A });
  function _($) {
    var O;
    let H = K?.Parent ? new w() : this;
    (Y(H, $), (O = H._zod).deferred ?? (O.deferred = []));
    for (let j of H._zod.deferred) j();
    return H;
  }
  return (
    Object.defineProperty(_, "init", { value: Y }),
    Object.defineProperty(_, Symbol.hasInstance, {
      value: ($) => {
        if (K?.Parent && $ instanceof K.Parent) return !0;
        return $?._zod?.traits?.has(A);
      },
    }),
    Object.defineProperty(_, "name", { value: A }),
    _
  );
}
function bJ(A) {
  if (A) Object.assign(nk6, A);
  return nk6;
}
var rk6, Yx1, ep, nk6;
var K$6 = E(() => {
  rk6 = Object.freeze({ status: "aborted" });
  Yx1 = Symbol("zod_brand");
  ep = class ep extends Error {
    constructor() {
      super(
        "Encountered Promise during synchronous parse. Use .parseAsync() instead.",
      );
    }
  };
  nk6 = {};
});
var u7 = {};
s1(u7, {
  unwrapMessage: () => ok6,
  stringifyPrimitive: () => g7,
  required: () => ndq,
  randomString: () => gdq,
  propertyKeyTypes: () => ek6,
  promiseAllObject: () => Bdq,
  primitiveTypes: () => Hx1,
  prefixIssues: () => BT,
  pick: () => Udq,
  partial: () => idq,
  optionalKeys: () => jx1,
  omit: () => ddq,
  numKeys: () => Fdq,
  nullish: () => Ar,
  normalizeParams: () => V7,
  merge: () => ldq,
  jsonStringifyReplacer: () => wx1,
  joinValues: () => XA,
  issue: () => Xx1,
  isPlainObject: () => z$6,
  isObject: () => Y$6,
  getSizableOrigin: () => AE6,
  getParsedType: () => pdq,
  getLengthableOrigin: () => qE6,
  getEnumValues: () => ak6,
  getElementAtPath: () => mdq,
  floatSafeRemainder: () => _x1,
  finalizeIssue: () => vv,
  extend: () => cdq,
  escapeRegex: () => AQ,
  esc: () => lA6,
  defineLazy: () => Uz,
  createTransparentProxy: () => Qdq,
  clone: () => Vv,
  cleanRegex: () => tk6,
  cleanEnum: () => rdq,
  captureStackTrace: () => hs6,
  cached: () => sk6,
  assignProp: () => $x1,
  assertNotEqual: () => Idq,
  assertNever: () => bdq,
  assertIs: () => xdq,
  assertEqual: () => hdq,
  assert: () => udq,
  allowsEval: () => Ox1,
  aborted: () => iA6,
  NUMBER_FORMAT_RANGES: () => Jx1,
  Class: () => ve8,
  BIGINT_FORMAT_RANGES: () => Dx1,
});
function hdq(A) {
  return A;
}
function Idq(A) {
  return A;
}
function xdq(A) {}
function bdq(A) {
  throw Error();
}
function udq(A) {}
function ak6(A) {
  let q = Object.values(A).filter((Y) => typeof Y === "number");
  return Object.entries(A)
    .filter(([Y, z]) => q.indexOf(+Y) === -1)
    .map(([Y, z]) => z);
}
function XA(A, q = "|") {
  return A.map((K) => g7(K)).join(q);
}
function wx1(A, q) {
  if (typeof q === "bigint") return q.toString();
  return q;
}
function sk6(A) {
  return {
    get value() {
      {
        let K = A();
        return (Object.defineProperty(this, "value", { value: K }), K);
      }
      throw Error("cached value already set");
    },
  };
}
function Ar(A) {
  return A === null || A === void 0;
}
function tk6(A) {
  let q = A.startsWith("^") ? 1 : 0,
    K = A.endsWith("$") ? A.length - 1 : A.length;
  return A.slice(q, K);
}
function _x1(A, q) {
  let K = (A.toString().split(".")[1] || "").length,
    Y = (q.toString().split(".")[1] || "").length,
    z = K > Y ? K : Y,
    w = Number.parseInt(A.toFixed(z).replace(".", "")),
    _ = Number.parseInt(q.toFixed(z).replace(".", ""));
  return (w % _) / 10 ** z;
}
function Uz(A, q, K) {
  Object.defineProperty(A, q, {
    get() {
      {
        let z = K();
        return ((A[q] = z), z);
      }
      throw Error("cached value already set");
    },
    set(z) {
      Object.defineProperty(A, q, { value: z });
    },
    configurable: !0,
  });
}
function $x1(A, q, K) {
  Object.defineProperty(A, q, {
    value: K,
    writable: !0,
    enumerable: !0,
    configurable: !0,
  });
}
function mdq(A, q) {
  if (!q) return A;
  return q.reduce((K, Y) => K?.[Y], A);
}
function Bdq(A) {
  let q = Object.keys(A),
    K = q.map((Y) => A[Y]);
  return Promise.all(K).then((Y) => {
    let z = {};
    for (let w = 0; w < q.length; w++) z[q[w]] = Y[w];
    return z;
  });
}
function gdq(A = 10) {
  let K = "";
  for (let Y = 0; Y < A; Y++)
    K += "abcdefghijklmnopqrstuvwxyz"[Math.floor(Math.random() * 26)];
  return K;
}
function lA6(A) {
  return JSON.stringify(A);
}
function Y$6(A) {
  return typeof A === "object" && A !== null && !Array.isArray(A);
}
function z$6(A) {
  if (Y$6(A) === !1) return !1;
  let q = A.constructor;
  if (q === void 0) return !0;
  let K = q.prototype;
  if (Y$6(K) === !1) return !1;
  if (Object.prototype.hasOwnProperty.call(K, "isPrototypeOf") === !1)
    return !1;
  return !0;
}
function Fdq(A) {
  let q = 0;
  for (let K in A) if (Object.prototype.hasOwnProperty.call(A, K)) q++;
  return q;
}
function AQ(A) {
  return A.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
function Vv(A, q, K) {
  let Y = new A._zod.constr(q ?? A._zod.def);
  if (!q || K?.parent) Y._zod.parent = A;
  return Y;
}
function V7(A) {
  let q = A;
  if (!q) return {};
  if (typeof q === "string") return { error: () => q };
  if (q?.message !== void 0) {
    if (q?.error !== void 0)
      throw Error("Cannot specify both `message` and `error` params");
    q.error = q.message;
  }
  if ((delete q.message, typeof q.error === "string"))
    return { ...q, error: () => q.error };
  return q;
}
function Qdq(A) {
  let q;
  return new Proxy(
    {},
    {
      get(K, Y, z) {
        return (q ?? (q = A()), Reflect.get(q, Y, z));
      },
      set(K, Y, z, w) {
        return (q ?? (q = A()), Reflect.set(q, Y, z, w));
      },
      has(K, Y) {
        return (q ?? (q = A()), Reflect.has(q, Y));
      },
      deleteProperty(K, Y) {
        return (q ?? (q = A()), Reflect.deleteProperty(q, Y));
      },
      ownKeys(K) {
        return (q ?? (q = A()), Reflect.ownKeys(q));
      },
      getOwnPropertyDescriptor(K, Y) {
        return (q ?? (q = A()), Reflect.getOwnPropertyDescriptor(q, Y));
      },
      defineProperty(K, Y, z) {
        return (q ?? (q = A()), Reflect.defineProperty(q, Y, z));
      },
    },
  );
}
function g7(A) {
  if (typeof A === "bigint") return A.toString() + "n";
  if (typeof A === "string") return `"${A}"`;
  return `${A}`;
}
function jx1(A) {
  return Object.keys(A).filter((q) => {
    return A[q]._zod.optin === "optional" && A[q]._zod.optout === "optional";
  });
}
function Udq(A, q) {
  let K = {},
    Y = A._zod.def;
  for (let z in q) {
    if (!(z in Y.shape)) throw Error(`Unrecognized key: "${z}"`);
    if (!q[z]) continue;
    K[z] = Y.shape[z];
  }
  return Vv(A, { ...A._zod.def, shape: K, checks: [] });
}
function ddq(A, q) {
  let K = { ...A._zod.def.shape },
    Y = A._zod.def;
  for (let z in q) {
    if (!(z in Y.shape)) throw Error(`Unrecognized key: "${z}"`);
    if (!q[z]) continue;
    delete K[z];
  }
  return Vv(A, { ...A._zod.def, shape: K, checks: [] });
}
function cdq(A, q) {
  if (!z$6(q)) throw Error("Invalid input to extend: expected a plain object");
  let K = {
    ...A._zod.def,
    get shape() {
      let Y = { ...A._zod.def.shape, ...q };
      return ($x1(this, "shape", Y), Y);
    },
    checks: [],
  };
  return Vv(A, K);
}
function ldq(A, q) {
  return Vv(A, {
    ...A._zod.def,
    get shape() {
      let K = { ...A._zod.def.shape, ...q._zod.def.shape };
      return ($x1(this, "shape", K), K);
    },
    catchall: q._zod.def.catchall,
    checks: [],
  });
}
function idq(A, q, K) {
  let Y = q._zod.def.shape,
    z = { ...Y };
  if (K)
    for (let w in K) {
      if (!(w in Y)) throw Error(`Unrecognized key: "${w}"`);
      if (!K[w]) continue;
      z[w] = A ? new A({ type: "optional", innerType: Y[w] }) : Y[w];
    }
  else
    for (let w in Y)
      z[w] = A ? new A({ type: "optional", innerType: Y[w] }) : Y[w];
  return Vv(q, { ...q._zod.def, shape: z, checks: [] });
}
function ndq(A, q, K) {
  let Y = q._zod.def.shape,
    z = { ...Y };
  if (K)
    for (let w in K) {
      if (!(w in z)) throw Error(`Unrecognized key: "${w}"`);
      if (!K[w]) continue;
      z[w] = new A({ type: "nonoptional", innerType: Y[w] });
    }
  else for (let w in Y) z[w] = new A({ type: "nonoptional", innerType: Y[w] });
  return Vv(q, { ...q._zod.def, shape: z, checks: [] });
}
function iA6(A, q = 0) {
  for (let K = q; K < A.issues.length; K++)
    if (A.issues[K]?.continue !== !0) return !0;
  return !1;
}
function BT(A, q) {
  return q.map((K) => {
    var Y;
    return ((Y = K).path ?? (Y.path = []), K.path.unshift(A), K);
  });
}
function ok6(A) {
  return typeof A === "string" ? A : A?.message;
}
function vv(A, q, K) {
  let Y = { ...A, path: A.path ?? [] };
  if (!A.message) {
    let z =
      ok6(A.inst?._zod.def?.error?.(A)) ??
      ok6(q?.error?.(A)) ??
      ok6(K.customError?.(A)) ??
      ok6(K.localeError?.(A)) ??
      "Invalid input";
    Y.message = z;
  }
  if ((delete Y.inst, delete Y.continue, !q?.reportInput)) delete Y.input;
  return Y;
}
function AE6(A) {
  if (A instanceof Set) return "set";
  if (A instanceof Map) return "map";
  if (A instanceof File) return "file";
  return "unknown";
}
function qE6(A) {
  if (Array.isArray(A)) return "array";
  if (typeof A === "string") return "string";
  return "unknown";
}
function Xx1(...A) {
  let [q, K, Y] = A;
  if (typeof q === "string")
    return { message: q, code: "custom", input: K, inst: Y };
  return { ...q };
}
function rdq(A) {
  return Object.entries(A)
    .filter(([q, K]) => {
      return Number.isNaN(Number.parseInt(q, 10));
    })
    .map((q) => q[1]);
}
class ve8 {
  constructor(...A) {}
}
var hs6,
  Ox1,
  pdq = (A) => {
    let q = typeof A;
    switch (q) {
      case "undefined":
        return "undefined";
      case "string":
        return "string";
      case "number":
        return Number.isNaN(A) ? "nan" : "number";
      case "boolean":
        return "boolean";
      case "function":
        return "function";
      case "bigint":
        return "bigint";
      case "symbol":
        return "symbol";
      case "object":
        if (Array.isArray(A)) return "array";
        if (A === null) return "null";
        if (
          A.then &&
          typeof A.then === "function" &&
          A.catch &&
          typeof A.catch === "function"
        )
          return "promise";
        if (typeof Map < "u" && A instanceof Map) return "map";
        if (typeof Set < "u" && A instanceof Set) return "set";
        if (typeof Date < "u" && A instanceof Date) return "date";
        if (typeof File < "u" && A instanceof File) return "file";
        return "object";
      default:
        throw Error(`Unknown data type: ${q}`);
    }
  },
  ek6,
  Hx1,
  Jx1,
  Dx1;
var A3 = E(() => {
  hs6 = Error.captureStackTrace ? Error.captureStackTrace : (...A) => {};
  Ox1 = sk6(() => {
    if (typeof navigator < "u" && navigator?.userAgent?.includes("Cloudflare"))
      return !1;
    try {
      return (new Function(""), !0);
    } catch (A) {
      return !1;
    }
  });
  ((ek6 = new Set(["string", "number", "symbol"])),
    (Hx1 = new Set([
      "string",
      "number",
      "bigint",
      "boolean",
      "symbol",
      "undefined",
    ])));
  ((Jx1 = {
    safeint: [Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER],
    int32: [-2147483648, 2147483647],
    uint32: [0, 4294967295],
    float32: [
      -340282346638528860000000000000000000000,
      340282346638528860000000000000000000000,
    ],
    float64: [-Number.MAX_VALUE, Number.MAX_VALUE],
  }),
    (Dx1 = {
      int64: [BigInt("-9223372036854775808"), BigInt("9223372036854775807")],
      uint64: [BigInt(0), BigInt("18446744073709551615")],
    }));
});
function YE6(A, q = (K) => K.message) {
  let K = {},
    Y = [];
  for (let z of A.issues)
    if (z.path.length > 0)
      ((K[z.path[0]] = K[z.path[0]] || []), K[z.path[0]].push(q(z)));
    else Y.push(q(z));
  return { formErrors: Y, fieldErrors: K };
}
function zE6(A, q) {
  let K =
      q ||
      function (w) {
        return w.message;
      },
    Y = { _errors: [] },
    z = (w) => {
      for (let _ of w.issues)
        if (_.code === "invalid_union" && _.errors.length)
          _.errors.map(($) => z({ issues: $ }));
        else if (_.code === "invalid_key") z({ issues: _.issues });
        else if (_.code === "invalid_element") z({ issues: _.issues });
        else if (_.path.length === 0) Y._errors.push(K(_));
        else {
          let $ = Y,
            O = 0;
          while (O < _.path.length) {
            let H = _.path[O];
            if (O !== _.path.length - 1) $[H] = $[H] || { _errors: [] };
            else (($[H] = $[H] || { _errors: [] }), $[H]._errors.push(K(_)));
            (($ = $[H]), O++);
          }
        }
    };
  return (z(A), Y);
}
function Mx1(A, q) {
  let K =
      q ||
      function (w) {
        return w.message;
      },
    Y = { errors: [] },
    z = (w, _ = []) => {
      var $, O;
      for (let H of w.issues)
        if (H.code === "invalid_union" && H.errors.length)
          H.errors.map((j) => z({ issues: j }, H.path));
        else if (H.code === "invalid_key") z({ issues: H.issues }, H.path);
        else if (H.code === "invalid_element") z({ issues: H.issues }, H.path);
        else {
          let j = [..._, ...H.path];
          if (j.length === 0) {
            Y.errors.push(K(H));
            continue;
          }
          let J = Y,
            D = 0;
          while (D < j.length) {
            let X = j[D],
              M = D === j.length - 1;
            if (typeof X === "string")
              (J.properties ?? (J.properties = {}),
                ($ = J.properties)[X] ?? ($[X] = { errors: [] }),
                (J = J.properties[X]));
            else
              (J.items ?? (J.items = []),
                (O = J.items)[X] ?? (O[X] = { errors: [] }),
                (J = J.items[X]));
            if (M) J.errors.push(K(H));
            D++;
          }
        }
    };
  return (z(A), Y);
}
function Ee8(A) {
  let q = [];
  for (let K of A)
    if (typeof K === "number") q.push(`[${K}]`);
    else if (typeof K === "symbol") q.push(`[${JSON.stringify(String(K))}]`);
    else if (/[^\w$]/.test(K)) q.push(`[${JSON.stringify(K)}]`);
    else {
      if (q.length) q.push(".");
      q.push(K);
    }
  return q.join("");
}
function Px1(A) {
  let q = [],
    K = [...A.issues].sort((Y, z) => Y.path.length - z.path.length);
  for (let Y of K)
    if ((q.push(`✖ ${Y.message}`), Y.path?.length))
      q.push(`  → at ${Ee8(Y.path)}`);
  return q.join(`
`);
}
var ke8 = (A, q) => {
    ((A.name = "$ZodError"),
      Object.defineProperty(A, "_zod", { value: A._zod, enumerable: !1 }),
      Object.defineProperty(A, "issues", { value: q, enumerable: !1 }),
      Object.defineProperty(A, "message", {
        get() {
          return JSON.stringify(q, wx1, 2);
        },
        enumerable: !0,
      }));
  },
  KE6,
  w$6;
var Wx1 = E(() => {
  K$6();
  A3();
  ((KE6 = f8("$ZodError", ke8)),
    (w$6 = f8("$ZodError", ke8, { Parent: Error })));
});
var Is6 = (A) => (q, K, Y, z) => {
    let w = Y ? Object.assign(Y, { async: !1 }) : { async: !1 },
      _ = q._zod.run({ value: K, issues: [] }, w);
    if (_ instanceof Promise) throw new ep();
    if (_.issues.length) {
      let $ = new (z?.Err ?? A)(_.issues.map((O) => vv(O, w, bJ())));
      throw (hs6($, z?.callee), $);
    }
    return _.value;
  },
  wE6,
  xs6 = (A) => async (q, K, Y, z) => {
    let w = Y ? Object.assign(Y, { async: !0 }) : { async: !0 },
      _ = q._zod.run({ value: K, issues: [] }, w);
    if (_ instanceof Promise) _ = await _;
    if (_.issues.length) {
      let $ = new (z?.Err ?? A)(_.issues.map((O) => vv(O, w, bJ())));
      throw (hs6($, z?.callee), $);
    }
    return _.value;
  },
  _E6,
  bs6 = (A) => (q, K, Y) => {
    let z = Y ? { ...Y, async: !1 } : { async: !1 },
      w = q._zod.run({ value: K, issues: [] }, z);
    if (w instanceof Promise) throw new ep();
    return w.issues.length
      ? {
          success: !1,
          error: new (A ?? KE6)(w.issues.map((_) => vv(_, z, bJ()))),
        }
      : { success: !0, data: w.value };
  },
  _$6,
  us6 = (A) => async (q, K, Y) => {
    let z = Y ? Object.assign(Y, { async: !0 }) : { async: !0 },
      w = q._zod.run({ value: K, issues: [] }, z);
    if (w instanceof Promise) w = await w;
    return w.issues.length
      ? { success: !1, error: new A(w.issues.map((_) => vv(_, z, bJ()))) }
      : { success: !0, data: w.value };
  },
  $E6;
var ms6 = E(() => {
  K$6();
  Wx1();
  A3();
  ((wE6 = Is6(w$6)), (_E6 = xs6(w$6)), (_$6 = bs6(w$6)), ($E6 = us6(w$6)));
});
var rA6 = {};
s1(rA6, {
  xid: () => Tx1,
  uuid7: () => edq,
  uuid6: () => tdq,
  uuid4: () => sdq,
  uuid: () => nA6,
  uppercase: () => lx1,
  unicodeEmail: () => Kcq,
  undefined: () => dx1,
  ulid: () => fx1,
  time: () => ux1,
  string: () => Bx1,
  rfc5322Email: () => qcq,
  number: () => px1,
  null: () => Ux1,
  nanoid: () => Vx1,
  lowercase: () => cx1,
  ksuid: () => Nx1,
  ipv6: () => Rx1,
  ipv4: () => yx1,
  integer: () => Fx1,
  html5Email: () => Acq,
  hostname: () => Ix1,
  guid: () => kx1,
  extendedDuration: () => adq,
  emoji: () => Lx1,
  email: () => Ex1,
  e164: () => xx1,
  duration: () => vx1,
  domain: () => wcq,
  datetime: () => mx1,
  date: () => bx1,
  cuid2: () => Zx1,
  cuid: () => Gx1,
  cidrv6: () => Sx1,
  cidrv4: () => Cx1,
  browserEmail: () => Ycq,
  boolean: () => Qx1,
  bigint: () => gx1,
  base64url: () => Bs6,
  base64: () => hx1,
  _emoji: () => zcq,
});
function Lx1() {
  return new RegExp(
    "^(\\p{Extended_Pictographic}|\\p{Emoji_Component})+$",
    "u",
  );
}
function ye8(A) {
  return typeof A.precision === "number"
    ? A.precision === -1
      ? "(?:[01]\\d|2[0-3]):[0-5]\\d"
      : A.precision === 0
        ? "(?:[01]\\d|2[0-3]):[0-5]\\d:[0-5]\\d"
        : `(?:[01]\\d|2[0-3]):[0-5]\\d:[0-5]\\d\\.\\d{${A.precision}}`
    : "(?:[01]\\d|2[0-3]):[0-5]\\d(?::[0-5]\\d(?:\\.\\d+)?)?";
}
function ux1(A) {
  return new RegExp(`^${ye8(A)}$`);
}
function mx1(A) {
  let q = ye8({ precision: A.precision }),
    K = ["Z"];
  if (A.local) K.push("");
  if (A.offset) K.push("([+-]\\d{2}:\\d{2})");
  let Y = `${q}(?:${K.join("|")})`;
  return new RegExp(`^${Le8}T(?:${Y})$`);
}
var Gx1,
  Zx1,
  fx1,
  Tx1,
  Nx1,
  Vx1,
  vx1,
  adq,
  kx1,
  nA6 = (A) => {
    if (!A)
      return /^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000)$/;
    return new RegExp(
      `^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-${A}[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12})$`,
    );
  },
  sdq,
  tdq,
  edq,
  Ex1,
  Acq,
  qcq,
  Kcq,
  Ycq,
  zcq = "^(\\p{Extended_Pictographic}|\\p{Emoji_Component})+$",
  yx1,
  Rx1,
  Cx1,
  Sx1,
  hx1,
  Bs6,
  Ix1,
  wcq,
  xx1,
  Le8 =
    "(?:(?:\\d\\d[2468][048]|\\d\\d[13579][26]|\\d\\d0[48]|[02468][048]00|[13579][26]00)-02-29|\\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\\d|30)|(?:02)-(?:0[1-9]|1\\d|2[0-8])))",
  bx1,
  Bx1 = (A) => {
    let q = A
      ? `[\\s\\S]{${A?.minimum ?? 0},${A?.maximum ?? ""}}`
      : "[\\s\\S]*";
    return new RegExp(`^${q}$`);
  },
  gx1,
  Fx1,
  px1,
  Qx1,
  Ux1,
  dx1,
  cx1,
  lx1;
var gs6 = E(() => {
  ((Gx1 = /^[cC][^\s-]{8,}$/),
    (Zx1 = /^[0-9a-z]+$/),
    (fx1 = /^[0-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{26}$/),
    (Tx1 = /^[0-9a-vA-V]{20}$/),
    (Nx1 = /^[A-Za-z0-9]{27}$/),
    (Vx1 = /^[a-zA-Z0-9_-]{21}$/),
    (vx1 =
      /^P(?:(\d+W)|(?!.*W)(?=\d|T\d)(\d+Y)?(\d+M)?(\d+D)?(T(?=\d)(\d+H)?(\d+M)?(\d+([.,]\d+)?S)?)?)$/),
    (adq =
      /^[-+]?P(?!$)(?:(?:[-+]?\d+Y)|(?:[-+]?\d+[.,]\d+Y$))?(?:(?:[-+]?\d+M)|(?:[-+]?\d+[.,]\d+M$))?(?:(?:[-+]?\d+W)|(?:[-+]?\d+[.,]\d+W$))?(?:(?:[-+]?\d+D)|(?:[-+]?\d+[.,]\d+D$))?(?:T(?=[\d+-])(?:(?:[-+]?\d+H)|(?:[-+]?\d+[.,]\d+H$))?(?:(?:[-+]?\d+M)|(?:[-+]?\d+[.,]\d+M$))?(?:[-+]?\d+(?:[.,]\d+)?S)?)??$/),
    (kx1 =
      /^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$/),
    (sdq = nA6(4)),
    (tdq = nA6(6)),
    (edq = nA6(7)),
    (Ex1 =
      /^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$/),
    (Acq =
      /^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/),
    (qcq =
      /^(([^<>()\[\]\\.,;:\s@"]+(\.[^<>()\[\]\\.,;:\s@"]+)*)|(".+"))@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$/),
    (Kcq = /^[^\s@"]{1,64}@[^\s@]{1,255}$/u),
    (Ycq =
      /^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/));
  ((yx1 =
    /^(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])$/),
    (Rx1 =
      /^(([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|::|([0-9a-fA-F]{1,4})?::([0-9a-fA-F]{1,4}:?){0,6})$/),
    (Cx1 =
      /^((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\/([0-9]|[1-2][0-9]|3[0-2])$/),
    (Sx1 =
      /^(([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|::|([0-9a-fA-F]{1,4})?::([0-9a-fA-F]{1,4}:?){0,6})\/(12[0-8]|1[01][0-9]|[1-9]?[0-9])$/),
    (hx1 =
      /^$|^(?:[0-9a-zA-Z+/]{4})*(?:(?:[0-9a-zA-Z+/]{2}==)|(?:[0-9a-zA-Z+/]{3}=))?$/),
    (Bs6 = /^[A-Za-z0-9_-]*$/),
    (Ix1 = /^([a-zA-Z0-9-]+\.)*[a-zA-Z0-9-]+$/),
    (wcq = /^([a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/),
    (xx1 = /^\+(?:[0-9]){6,14}[0-9]$/),
    (bx1 = new RegExp(`^${Le8}$`)));
  ((gx1 = /^\d+n?$/),
    (Fx1 = /^\d+$/),
    (px1 = /^-?\d+(?:\.\d+)?/i),
    (Qx1 = /true|false/i),
    (Ux1 = /null/i),
    (dx1 = /undefined/i),
    (cx1 = /^[^A-Z]*$/),
    (lx1 = /^[^a-z]*$/));
});
function Re8(A, q, K) {
  if (A.issues.length) q.issues.push(...BT(K, A.issues));
}
var cO,
  Ce8,
  Fs6,
  ps6,
  ix1,
  nx1,
  rx1,
  ox1,
  ax1,
  sx1,
  tx1,
  ex1,
  Ab1,
  $$6,
  qb1,
  Kb1,
  Yb1,
  zb1,
  wb1,
  _b1,
  $b1,
  Ob1,
  Hb1;
var Qs6 = E(() => {
  K$6();
  gs6();
  A3();
  ((cO = f8("$ZodCheck", (A, q) => {
    var K;
    (A._zod ?? (A._zod = {}),
      (A._zod.def = q),
      (K = A._zod).onattach ?? (K.onattach = []));
  })),
    (Ce8 = { number: "number", bigint: "bigint", object: "date" }),
    (Fs6 = f8("$ZodCheckLessThan", (A, q) => {
      cO.init(A, q);
      let K = Ce8[typeof q.value];
      (A._zod.onattach.push((Y) => {
        let z = Y._zod.bag,
          w =
            (q.inclusive ? z.maximum : z.exclusiveMaximum) ??
            Number.POSITIVE_INFINITY;
        if (q.value < w)
          if (q.inclusive) z.maximum = q.value;
          else z.exclusiveMaximum = q.value;
      }),
        (A._zod.check = (Y) => {
          if (q.inclusive ? Y.value <= q.value : Y.value < q.value) return;
          Y.issues.push({
            origin: K,
            code: "too_big",
            maximum: q.value,
            input: Y.value,
            inclusive: q.inclusive,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (ps6 = f8("$ZodCheckGreaterThan", (A, q) => {
      cO.init(A, q);
      let K = Ce8[typeof q.value];
      (A._zod.onattach.push((Y) => {
        let z = Y._zod.bag,
          w =
            (q.inclusive ? z.minimum : z.exclusiveMinimum) ??
            Number.NEGATIVE_INFINITY;
        if (q.value > w)
          if (q.inclusive) z.minimum = q.value;
          else z.exclusiveMinimum = q.value;
      }),
        (A._zod.check = (Y) => {
          if (q.inclusive ? Y.value >= q.value : Y.value > q.value) return;
          Y.issues.push({
            origin: K,
            code: "too_small",
            minimum: q.value,
            input: Y.value,
            inclusive: q.inclusive,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (ix1 = f8("$ZodCheckMultipleOf", (A, q) => {
      (cO.init(A, q),
        A._zod.onattach.push((K) => {
          var Y;
          (Y = K._zod.bag).multipleOf ?? (Y.multipleOf = q.value);
        }),
        (A._zod.check = (K) => {
          if (typeof K.value !== typeof q.value)
            throw Error("Cannot mix number and bigint in multiple_of check.");
          if (
            typeof K.value === "bigint"
              ? K.value % q.value === BigInt(0)
              : _x1(K.value, q.value) === 0
          )
            return;
          K.issues.push({
            origin: typeof K.value,
            code: "not_multiple_of",
            divisor: q.value,
            input: K.value,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (nx1 = f8("$ZodCheckNumberFormat", (A, q) => {
      (cO.init(A, q), (q.format = q.format || "float64"));
      let K = q.format?.includes("int"),
        Y = K ? "int" : "number",
        [z, w] = Jx1[q.format];
      (A._zod.onattach.push((_) => {
        let $ = _._zod.bag;
        if ((($.format = q.format), ($.minimum = z), ($.maximum = w), K))
          $.pattern = Fx1;
      }),
        (A._zod.check = (_) => {
          let $ = _.value;
          if (K) {
            if (!Number.isInteger($)) {
              _.issues.push({
                expected: Y,
                format: q.format,
                code: "invalid_type",
                input: $,
                inst: A,
              });
              return;
            }
            if (!Number.isSafeInteger($)) {
              if ($ > 0)
                _.issues.push({
                  input: $,
                  code: "too_big",
                  maximum: Number.MAX_SAFE_INTEGER,
                  note: "Integers must be within the safe integer range.",
                  inst: A,
                  origin: Y,
                  continue: !q.abort,
                });
              else
                _.issues.push({
                  input: $,
                  code: "too_small",
                  minimum: Number.MIN_SAFE_INTEGER,
                  note: "Integers must be within the safe integer range.",
                  inst: A,
                  origin: Y,
                  continue: !q.abort,
                });
              return;
            }
          }
          if ($ < z)
            _.issues.push({
              origin: "number",
              input: $,
              code: "too_small",
              minimum: z,
              inclusive: !0,
              inst: A,
              continue: !q.abort,
            });
          if ($ > w)
            _.issues.push({
              origin: "number",
              input: $,
              code: "too_big",
              maximum: w,
              inst: A,
            });
        }));
    })),
    (rx1 = f8("$ZodCheckBigIntFormat", (A, q) => {
      cO.init(A, q);
      let [K, Y] = Dx1[q.format];
      (A._zod.onattach.push((z) => {
        let w = z._zod.bag;
        ((w.format = q.format), (w.minimum = K), (w.maximum = Y));
      }),
        (A._zod.check = (z) => {
          let w = z.value;
          if (w < K)
            z.issues.push({
              origin: "bigint",
              input: w,
              code: "too_small",
              minimum: K,
              inclusive: !0,
              inst: A,
              continue: !q.abort,
            });
          if (w > Y)
            z.issues.push({
              origin: "bigint",
              input: w,
              code: "too_big",
              maximum: Y,
              inst: A,
            });
        }));
    })),
    (ox1 = f8("$ZodCheckMaxSize", (A, q) => {
      (cO.init(A, q),
        (A._zod.when = (K) => {
          let Y = K.value;
          return !Ar(Y) && Y.size !== void 0;
        }),
        A._zod.onattach.push((K) => {
          let Y = K._zod.bag.maximum ?? Number.POSITIVE_INFINITY;
          if (q.maximum < Y) K._zod.bag.maximum = q.maximum;
        }),
        (A._zod.check = (K) => {
          let Y = K.value;
          if (Y.size <= q.maximum) return;
          K.issues.push({
            origin: AE6(Y),
            code: "too_big",
            maximum: q.maximum,
            input: Y,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (ax1 = f8("$ZodCheckMinSize", (A, q) => {
      (cO.init(A, q),
        (A._zod.when = (K) => {
          let Y = K.value;
          return !Ar(Y) && Y.size !== void 0;
        }),
        A._zod.onattach.push((K) => {
          let Y = K._zod.bag.minimum ?? Number.NEGATIVE_INFINITY;
          if (q.minimum > Y) K._zod.bag.minimum = q.minimum;
        }),
        (A._zod.check = (K) => {
          let Y = K.value;
          if (Y.size >= q.minimum) return;
          K.issues.push({
            origin: AE6(Y),
            code: "too_small",
            minimum: q.minimum,
            input: Y,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (sx1 = f8("$ZodCheckSizeEquals", (A, q) => {
      (cO.init(A, q),
        (A._zod.when = (K) => {
          let Y = K.value;
          return !Ar(Y) && Y.size !== void 0;
        }),
        A._zod.onattach.push((K) => {
          let Y = K._zod.bag;
          ((Y.minimum = q.size), (Y.maximum = q.size), (Y.size = q.size));
        }),
        (A._zod.check = (K) => {
          let Y = K.value,
            z = Y.size;
          if (z === q.size) return;
          let w = z > q.size;
          K.issues.push({
            origin: AE6(Y),
            ...(w
              ? { code: "too_big", maximum: q.size }
              : { code: "too_small", minimum: q.size }),
            inclusive: !0,
            exact: !0,
            input: K.value,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (tx1 = f8("$ZodCheckMaxLength", (A, q) => {
      (cO.init(A, q),
        (A._zod.when = (K) => {
          let Y = K.value;
          return !Ar(Y) && Y.length !== void 0;
        }),
        A._zod.onattach.push((K) => {
          let Y = K._zod.bag.maximum ?? Number.POSITIVE_INFINITY;
          if (q.maximum < Y) K._zod.bag.maximum = q.maximum;
        }),
        (A._zod.check = (K) => {
          let Y = K.value;
          if (Y.length <= q.maximum) return;
          let w = qE6(Y);
          K.issues.push({
            origin: w,
            code: "too_big",
            maximum: q.maximum,
            inclusive: !0,
            input: Y,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (ex1 = f8("$ZodCheckMinLength", (A, q) => {
      (cO.init(A, q),
        (A._zod.when = (K) => {
          let Y = K.value;
          return !Ar(Y) && Y.length !== void 0;
        }),
        A._zod.onattach.push((K) => {
          let Y = K._zod.bag.minimum ?? Number.NEGATIVE_INFINITY;
          if (q.minimum > Y) K._zod.bag.minimum = q.minimum;
        }),
        (A._zod.check = (K) => {
          let Y = K.value;
          if (Y.length >= q.minimum) return;
          let w = qE6(Y);
          K.issues.push({
            origin: w,
            code: "too_small",
            minimum: q.minimum,
            inclusive: !0,
            input: Y,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (Ab1 = f8("$ZodCheckLengthEquals", (A, q) => {
      (cO.init(A, q),
        (A._zod.when = (K) => {
          let Y = K.value;
          return !Ar(Y) && Y.length !== void 0;
        }),
        A._zod.onattach.push((K) => {
          let Y = K._zod.bag;
          ((Y.minimum = q.length),
            (Y.maximum = q.length),
            (Y.length = q.length));
        }),
        (A._zod.check = (K) => {
          let Y = K.value,
            z = Y.length;
          if (z === q.length) return;
          let w = qE6(Y),
            _ = z > q.length;
          K.issues.push({
            origin: w,
            ...(_
              ? { code: "too_big", maximum: q.length }
              : { code: "too_small", minimum: q.length }),
            inclusive: !0,
            exact: !0,
            input: K.value,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    ($$6 = f8("$ZodCheckStringFormat", (A, q) => {
      var K, Y;
      if (
        (cO.init(A, q),
        A._zod.onattach.push((z) => {
          let w = z._zod.bag;
          if (((w.format = q.format), q.pattern))
            (w.patterns ?? (w.patterns = new Set()), w.patterns.add(q.pattern));
        }),
        q.pattern)
      )
        (K = A._zod).check ??
          (K.check = (z) => {
            if (((q.pattern.lastIndex = 0), q.pattern.test(z.value))) return;
            z.issues.push({
              origin: "string",
              code: "invalid_format",
              format: q.format,
              input: z.value,
              ...(q.pattern ? { pattern: q.pattern.toString() } : {}),
              inst: A,
              continue: !q.abort,
            });
          });
      else (Y = A._zod).check ?? (Y.check = () => {});
    })),
    (qb1 = f8("$ZodCheckRegex", (A, q) => {
      ($$6.init(A, q),
        (A._zod.check = (K) => {
          if (((q.pattern.lastIndex = 0), q.pattern.test(K.value))) return;
          K.issues.push({
            origin: "string",
            code: "invalid_format",
            format: "regex",
            input: K.value,
            pattern: q.pattern.toString(),
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (Kb1 = f8("$ZodCheckLowerCase", (A, q) => {
      (q.pattern ?? (q.pattern = cx1), $$6.init(A, q));
    })),
    (Yb1 = f8("$ZodCheckUpperCase", (A, q) => {
      (q.pattern ?? (q.pattern = lx1), $$6.init(A, q));
    })),
    (zb1 = f8("$ZodCheckIncludes", (A, q) => {
      cO.init(A, q);
      let K = AQ(q.includes),
        Y = new RegExp(
          typeof q.position === "number" ? `^.{${q.position}}${K}` : K,
        );
      ((q.pattern = Y),
        A._zod.onattach.push((z) => {
          let w = z._zod.bag;
          (w.patterns ?? (w.patterns = new Set()), w.patterns.add(Y));
        }),
        (A._zod.check = (z) => {
          if (z.value.includes(q.includes, q.position)) return;
          z.issues.push({
            origin: "string",
            code: "invalid_format",
            format: "includes",
            includes: q.includes,
            input: z.value,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (wb1 = f8("$ZodCheckStartsWith", (A, q) => {
      cO.init(A, q);
      let K = new RegExp(`^${AQ(q.prefix)}.*`);
      (q.pattern ?? (q.pattern = K),
        A._zod.onattach.push((Y) => {
          let z = Y._zod.bag;
          (z.patterns ?? (z.patterns = new Set()), z.patterns.add(K));
        }),
        (A._zod.check = (Y) => {
          if (Y.value.startsWith(q.prefix)) return;
          Y.issues.push({
            origin: "string",
            code: "invalid_format",
            format: "starts_with",
            prefix: q.prefix,
            input: Y.value,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (_b1 = f8("$ZodCheckEndsWith", (A, q) => {
      cO.init(A, q);
      let K = new RegExp(`.*${AQ(q.suffix)}$`);
      (q.pattern ?? (q.pattern = K),
        A._zod.onattach.push((Y) => {
          let z = Y._zod.bag;
          (z.patterns ?? (z.patterns = new Set()), z.patterns.add(K));
        }),
        (A._zod.check = (Y) => {
          if (Y.value.endsWith(q.suffix)) return;
          Y.issues.push({
            origin: "string",
            code: "invalid_format",
            format: "ends_with",
            suffix: q.suffix,
            input: Y.value,
            inst: A,
            continue: !q.abort,
          });
        }));
    })));
  (($b1 = f8("$ZodCheckProperty", (A, q) => {
    (cO.init(A, q),
      (A._zod.check = (K) => {
        let Y = q.schema._zod.run(
          { value: K.value[q.property], issues: [] },
          {},
        );
        if (Y instanceof Promise) return Y.then((z) => Re8(z, K, q.property));
        Re8(Y, K, q.property);
        return;
      }));
  })),
    (Ob1 = f8("$ZodCheckMimeType", (A, q) => {
      cO.init(A, q);
      let K = new Set(q.mime);
      (A._zod.onattach.push((Y) => {
        Y._zod.bag.mime = q.mime;
      }),
        (A._zod.check = (Y) => {
          if (K.has(Y.value.type)) return;
          Y.issues.push({
            code: "invalid_value",
            values: q.mime,
            input: Y.value.type,
            inst: A,
          });
        }));
    })),
    (Hb1 = f8("$ZodCheckOverwrite", (A, q) => {
      (cO.init(A, q),
        (A._zod.check = (K) => {
          K.value = q.tx(K.value);
        }));
    })));
});
class Us6 {
  constructor(A = []) {
    if (((this.content = []), (this.indent = 0), this)) this.args = A;
  }
  indented(A) {
    ((this.indent += 1), A(this), (this.indent -= 1));
  }
  write(A) {
    if (typeof A === "function") {
      (A(this, { execution: "sync" }), A(this, { execution: "async" }));
      return;
    }
    let K = A.split(
        `
`,
      ).filter((w) => w),
      Y = Math.min(...K.map((w) => w.length - w.trimStart().length)),
      z = K.map((w) => w.slice(Y)).map((w) => " ".repeat(this.indent * 2) + w);
    for (let w of z) this.content.push(w);
  }
  compile() {
    let A = Function,
      q = this?.args,
      Y = [...(this?.content ?? [""]).map((z) => `  ${z}`)];
    return new A(
      ...q,
      Y.join(`
`),
    );
  }
}
var jb1;
var Jb1 = E(() => {
  jb1 = { major: 4, minor: 0, patch: 0 };
});
function Ib1(A) {
  if (A === "") return !0;
  if (A.length % 4 !== 0) return !1;
  try {
    return (atob(A), !0);
  } catch {
    return !1;
  }
}
function Ue8(A) {
  if (!Bs6.test(A)) return !1;
  let q = A.replace(/[-_]/g, (Y) => (Y === "-" ? "+" : "/")),
    K = q.padEnd(Math.ceil(q.length / 4) * 4, "=");
  return Ib1(K);
}
function de8(A, q = null) {
  try {
    let K = A.split(".");
    if (K.length !== 3) return !1;
    let [Y] = K;
    if (!Y) return !1;
    let z = JSON.parse(atob(Y));
    if ("typ" in z && z?.typ !== "JWT") return !1;
    if (!z.alg) return !1;
    if (q && (!("alg" in z) || z.alg !== q)) return !1;
    return !0;
  } catch {
    return !1;
  }
}
function he8(A, q, K) {
  if (A.issues.length) q.issues.push(...BT(K, A.issues));
  q.value[K] = A.value;
}
function ds6(A, q, K) {
  if (A.issues.length) q.issues.push(...BT(K, A.issues));
  q.value[K] = A.value;
}
function Ie8(A, q, K, Y) {
  if (A.issues.length)
    if (Y[K] === void 0)
      if (K in Y) q.value[K] = void 0;
      else q.value[K] = A.value;
    else q.issues.push(...BT(K, A.issues));
  else if (A.value === void 0) {
    if (K in Y) q.value[K] = void 0;
  } else q.value[K] = A.value;
}
function xe8(A, q, K, Y) {
  for (let z of A) if (z.issues.length === 0) return ((q.value = z.value), q);
  return (
    q.issues.push({
      code: "invalid_union",
      input: q.value,
      inst: K,
      errors: A.map((z) => z.issues.map((w) => vv(w, Y, bJ()))),
    }),
    q
  );
}
function Db1(A, q) {
  if (A === q) return { valid: !0, data: A };
  if (A instanceof Date && q instanceof Date && +A === +q)
    return { valid: !0, data: A };
  if (z$6(A) && z$6(q)) {
    let K = Object.keys(q),
      Y = Object.keys(A).filter((w) => K.indexOf(w) !== -1),
      z = { ...A, ...q };
    for (let w of Y) {
      let _ = Db1(A[w], q[w]);
      if (!_.valid)
        return { valid: !1, mergeErrorPath: [w, ..._.mergeErrorPath] };
      z[w] = _.data;
    }
    return { valid: !0, data: z };
  }
  if (Array.isArray(A) && Array.isArray(q)) {
    if (A.length !== q.length) return { valid: !1, mergeErrorPath: [] };
    let K = [];
    for (let Y = 0; Y < A.length; Y++) {
      let z = A[Y],
        w = q[Y],
        _ = Db1(z, w);
      if (!_.valid)
        return { valid: !1, mergeErrorPath: [Y, ..._.mergeErrorPath] };
      K.push(_.data);
    }
    return { valid: !0, data: K };
  }
  return { valid: !1, mergeErrorPath: [] };
}
function be8(A, q, K) {
  if (q.issues.length) A.issues.push(...q.issues);
  if (K.issues.length) A.issues.push(...K.issues);
  if (iA6(A)) return A;
  let Y = Db1(q.value, K.value);
  if (!Y.valid)
    throw Error(
      `Unmergable intersection. Error path: ${JSON.stringify(Y.mergeErrorPath)}`,
    );
  return ((A.value = Y.data), A);
}
function cs6(A, q, K) {
  if (A.issues.length) q.issues.push(...BT(K, A.issues));
  q.value[K] = A.value;
}
function ue8(A, q, K, Y, z, w, _) {
  if (A.issues.length)
    if (ek6.has(typeof Y)) K.issues.push(...BT(Y, A.issues));
    else
      K.issues.push({
        origin: "map",
        code: "invalid_key",
        input: z,
        inst: w,
        issues: A.issues.map(($) => vv($, _, bJ())),
      });
  if (q.issues.length)
    if (ek6.has(typeof Y)) K.issues.push(...BT(Y, q.issues));
    else
      K.issues.push({
        origin: "map",
        code: "invalid_element",
        input: z,
        inst: w,
        key: Y,
        issues: q.issues.map(($) => vv($, _, bJ())),
      });
  K.value.set(A.value, q.value);
}
function me8(A, q) {
  if (A.issues.length) q.issues.push(...A.issues);
  q.value.add(A.value);
}
function Be8(A, q) {
  if (A.value === void 0) A.value = q.defaultValue;
  return A;
}
function ge8(A, q) {
  if (!A.issues.length && A.value === void 0)
    A.issues.push({
      code: "invalid_type",
      expected: "nonoptional",
      input: A.value,
      inst: q,
    });
  return A;
}
function Fe8(A, q, K) {
  if (iA6(A)) return A;
  return q.out._zod.run({ value: A.value, issues: A.issues }, K);
}
function pe8(A) {
  return ((A.value = Object.freeze(A.value)), A);
}
function Qe8(A, q, K, Y) {
  if (!A) {
    let z = {
      code: "custom",
      input: K,
      inst: Y,
      path: [...(Y._zod.def.path ?? [])],
      continue: !Y._zod.def.abort,
    };
    if (Y._zod.def.params) z.params = Y._zod.def.params;
    q.issues.push(Xx1(z));
  }
}
var T3,
  oA6,
  Uw,
  Xb1,
  Mb1,
  Pb1,
  Wb1,
  Gb1,
  Zb1,
  fb1,
  Tb1,
  Nb1,
  Vb1,
  vb1,
  kb1,
  Eb1,
  Lb1,
  yb1,
  Rb1,
  Cb1,
  Sb1,
  hb1,
  xb1,
  bb1,
  ub1,
  mb1,
  Bb1,
  ls6,
  gb1,
  OE6,
  is6,
  Fb1,
  pb1,
  Qb1,
  Ub1,
  db1,
  O$6,
  cb1,
  lb1,
  ib1,
  HE6,
  nb1,
  ns6,
  rb1,
  ob1,
  aA6,
  ab1,
  sb1,
  tb1,
  eb1,
  Au1,
  qu1,
  jE6,
  Ku1,
  Yu1,
  zu1,
  wu1,
  _u1,
  $u1,
  Ou1,
  Hu1,
  JE6,
  ju1,
  Ju1,
  Du1,
  Xu1,
  Mu1;
var DE6 = E(() => {
  Qs6();
  K$6();
  ms6();
  gs6();
  A3();
  Jb1();
  A3();
  ((T3 = f8("$ZodType", (A, q) => {
    var K;
    (A ?? (A = {}),
      (A._zod.def = q),
      (A._zod.bag = A._zod.bag || {}),
      (A._zod.version = jb1));
    let Y = [...(A._zod.def.checks ?? [])];
    if (A._zod.traits.has("$ZodCheck")) Y.unshift(A);
    for (let z of Y) for (let w of z._zod.onattach) w(A);
    if (Y.length === 0)
      ((K = A._zod).deferred ?? (K.deferred = []),
        A._zod.deferred?.push(() => {
          A._zod.run = A._zod.parse;
        }));
    else {
      let z = (w, _, $) => {
        let O = iA6(w),
          H;
        for (let j of _) {
          if (j._zod.when) {
            if (!j._zod.when(w)) continue;
          } else if (O) continue;
          let J = w.issues.length,
            D = j._zod.check(w);
          if (D instanceof Promise && $?.async === !1) throw new ep();
          if (H || D instanceof Promise)
            H = (H ?? Promise.resolve()).then(async () => {
              if ((await D, w.issues.length === J)) return;
              if (!O) O = iA6(w, J);
            });
          else {
            if (w.issues.length === J) continue;
            if (!O) O = iA6(w, J);
          }
        }
        if (H)
          return H.then(() => {
            return w;
          });
        return w;
      };
      A._zod.run = (w, _) => {
        let $ = A._zod.parse(w, _);
        if ($ instanceof Promise) {
          if (_.async === !1) throw new ep();
          return $.then((O) => z(O, Y, _));
        }
        return z($, Y, _);
      };
    }
    A["~standard"] = {
      validate: (z) => {
        try {
          let w = _$6(A, z);
          return w.success ? { value: w.data } : { issues: w.error?.issues };
        } catch (w) {
          return $E6(A, z).then((_) =>
            _.success ? { value: _.data } : { issues: _.error?.issues },
          );
        }
      },
      vendor: "zod",
      version: 1,
    };
  })),
    (oA6 = f8("$ZodString", (A, q) => {
      (T3.init(A, q),
        (A._zod.pattern =
          [...(A?._zod.bag?.patterns ?? [])].pop() ?? Bx1(A._zod.bag)),
        (A._zod.parse = (K, Y) => {
          if (q.coerce)
            try {
              K.value = String(K.value);
            } catch (z) {}
          if (typeof K.value === "string") return K;
          return (
            K.issues.push({
              expected: "string",
              code: "invalid_type",
              input: K.value,
              inst: A,
            }),
            K
          );
        }));
    })),
    (Uw = f8("$ZodStringFormat", (A, q) => {
      ($$6.init(A, q), oA6.init(A, q));
    })),
    (Xb1 = f8("$ZodGUID", (A, q) => {
      (q.pattern ?? (q.pattern = kx1), Uw.init(A, q));
    })),
    (Mb1 = f8("$ZodUUID", (A, q) => {
      if (q.version) {
        let Y = { v1: 1, v2: 2, v3: 3, v4: 4, v5: 5, v6: 6, v7: 7, v8: 8 }[
          q.version
        ];
        if (Y === void 0) throw Error(`Invalid UUID version: "${q.version}"`);
        q.pattern ?? (q.pattern = nA6(Y));
      } else q.pattern ?? (q.pattern = nA6());
      Uw.init(A, q);
    })),
    (Pb1 = f8("$ZodEmail", (A, q) => {
      (q.pattern ?? (q.pattern = Ex1), Uw.init(A, q));
    })),
    (Wb1 = f8("$ZodURL", (A, q) => {
      (Uw.init(A, q),
        (A._zod.check = (K) => {
          try {
            let Y = K.value,
              z = new URL(Y),
              w = z.href;
            if (q.hostname) {
              if (((q.hostname.lastIndex = 0), !q.hostname.test(z.hostname)))
                K.issues.push({
                  code: "invalid_format",
                  format: "url",
                  note: "Invalid hostname",
                  pattern: Ix1.source,
                  input: K.value,
                  inst: A,
                  continue: !q.abort,
                });
            }
            if (q.protocol) {
              if (
                ((q.protocol.lastIndex = 0),
                !q.protocol.test(
                  z.protocol.endsWith(":")
                    ? z.protocol.slice(0, -1)
                    : z.protocol,
                ))
              )
                K.issues.push({
                  code: "invalid_format",
                  format: "url",
                  note: "Invalid protocol",
                  pattern: q.protocol.source,
                  input: K.value,
                  inst: A,
                  continue: !q.abort,
                });
            }
            if (!Y.endsWith("/") && w.endsWith("/")) K.value = w.slice(0, -1);
            else K.value = w;
            return;
          } catch (Y) {
            K.issues.push({
              code: "invalid_format",
              format: "url",
              input: K.value,
              inst: A,
              continue: !q.abort,
            });
          }
        }));
    })),
    (Gb1 = f8("$ZodEmoji", (A, q) => {
      (q.pattern ?? (q.pattern = Lx1()), Uw.init(A, q));
    })),
    (Zb1 = f8("$ZodNanoID", (A, q) => {
      (q.pattern ?? (q.pattern = Vx1), Uw.init(A, q));
    })),
    (fb1 = f8("$ZodCUID", (A, q) => {
      (q.pattern ?? (q.pattern = Gx1), Uw.init(A, q));
    })),
    (Tb1 = f8("$ZodCUID2", (A, q) => {
      (q.pattern ?? (q.pattern = Zx1), Uw.init(A, q));
    })),
    (Nb1 = f8("$ZodULID", (A, q) => {
      (q.pattern ?? (q.pattern = fx1), Uw.init(A, q));
    })),
    (Vb1 = f8("$ZodXID", (A, q) => {
      (q.pattern ?? (q.pattern = Tx1), Uw.init(A, q));
    })),
    (vb1 = f8("$ZodKSUID", (A, q) => {
      (q.pattern ?? (q.pattern = Nx1), Uw.init(A, q));
    })),
    (kb1 = f8("$ZodISODateTime", (A, q) => {
      (q.pattern ?? (q.pattern = mx1(q)), Uw.init(A, q));
    })),
    (Eb1 = f8("$ZodISODate", (A, q) => {
      (q.pattern ?? (q.pattern = bx1), Uw.init(A, q));
    })),
    (Lb1 = f8("$ZodISOTime", (A, q) => {
      (q.pattern ?? (q.pattern = ux1(q)), Uw.init(A, q));
    })),
    (yb1 = f8("$ZodISODuration", (A, q) => {
      (q.pattern ?? (q.pattern = vx1), Uw.init(A, q));
    })),
    (Rb1 = f8("$ZodIPv4", (A, q) => {
      (q.pattern ?? (q.pattern = yx1),
        Uw.init(A, q),
        A._zod.onattach.push((K) => {
          let Y = K._zod.bag;
          Y.format = "ipv4";
        }));
    })),
    (Cb1 = f8("$ZodIPv6", (A, q) => {
      (q.pattern ?? (q.pattern = Rx1),
        Uw.init(A, q),
        A._zod.onattach.push((K) => {
          let Y = K._zod.bag;
          Y.format = "ipv6";
        }),
        (A._zod.check = (K) => {
          try {
            new URL(`http://[${K.value}]`);
          } catch {
            K.issues.push({
              code: "invalid_format",
              format: "ipv6",
              input: K.value,
              inst: A,
              continue: !q.abort,
            });
          }
        }));
    })),
    (Sb1 = f8("$ZodCIDRv4", (A, q) => {
      (q.pattern ?? (q.pattern = Cx1), Uw.init(A, q));
    })),
    (hb1 = f8("$ZodCIDRv6", (A, q) => {
      (q.pattern ?? (q.pattern = Sx1),
        Uw.init(A, q),
        (A._zod.check = (K) => {
          let [Y, z] = K.value.split("/");
          try {
            if (!z) throw Error();
            let w = Number(z);
            if (`${w}` !== z) throw Error();
            if (w < 0 || w > 128) throw Error();
            new URL(`http://[${Y}]`);
          } catch {
            K.issues.push({
              code: "invalid_format",
              format: "cidrv6",
              input: K.value,
              inst: A,
              continue: !q.abort,
            });
          }
        }));
    })));
  xb1 = f8("$ZodBase64", (A, q) => {
    (q.pattern ?? (q.pattern = hx1),
      Uw.init(A, q),
      A._zod.onattach.push((K) => {
        K._zod.bag.contentEncoding = "base64";
      }),
      (A._zod.check = (K) => {
        if (Ib1(K.value)) return;
        K.issues.push({
          code: "invalid_format",
          format: "base64",
          input: K.value,
          inst: A,
          continue: !q.abort,
        });
      }));
  });
  ((bb1 = f8("$ZodBase64URL", (A, q) => {
    (q.pattern ?? (q.pattern = Bs6),
      Uw.init(A, q),
      A._zod.onattach.push((K) => {
        K._zod.bag.contentEncoding = "base64url";
      }),
      (A._zod.check = (K) => {
        if (Ue8(K.value)) return;
        K.issues.push({
          code: "invalid_format",
          format: "base64url",
          input: K.value,
          inst: A,
          continue: !q.abort,
        });
      }));
  })),
    (ub1 = f8("$ZodE164", (A, q) => {
      (q.pattern ?? (q.pattern = xx1), Uw.init(A, q));
    })));
  ((mb1 = f8("$ZodJWT", (A, q) => {
    (Uw.init(A, q),
      (A._zod.check = (K) => {
        if (de8(K.value, q.alg)) return;
        K.issues.push({
          code: "invalid_format",
          format: "jwt",
          input: K.value,
          inst: A,
          continue: !q.abort,
        });
      }));
  })),
    (Bb1 = f8("$ZodCustomStringFormat", (A, q) => {
      (Uw.init(A, q),
        (A._zod.check = (K) => {
          if (q.fn(K.value)) return;
          K.issues.push({
            code: "invalid_format",
            format: q.format,
            input: K.value,
            inst: A,
            continue: !q.abort,
          });
        }));
    })),
    (ls6 = f8("$ZodNumber", (A, q) => {
      (T3.init(A, q),
        (A._zod.pattern = A._zod.bag.pattern ?? px1),
        (A._zod.parse = (K, Y) => {
          if (q.coerce)
            try {
              K.value = Number(K.value);
            } catch (_) {}
          let z = K.value;
          if (typeof z === "number" && !Number.isNaN(z) && Number.isFinite(z))
            return K;
          let w =
            typeof z === "number"
              ? Number.isNaN(z)
                ? "NaN"
                : !Number.isFinite(z)
                  ? "Infinity"
                  : void 0
              : void 0;
          return (
            K.issues.push({
              expected: "number",
              code: "invalid_type",
              input: z,
              inst: A,
              ...(w ? { received: w } : {}),
            }),
            K
          );
        }));
    })),
    (gb1 = f8("$ZodNumber", (A, q) => {
      (nx1.init(A, q), ls6.init(A, q));
    })),
    (OE6 = f8("$ZodBoolean", (A, q) => {
      (T3.init(A, q),
        (A._zod.pattern = Qx1),
        (A._zod.parse = (K, Y) => {
          if (q.coerce)
            try {
              K.value = Boolean(K.value);
            } catch (w) {}
          let z = K.value;
          if (typeof z === "boolean") return K;
          return (
            K.issues.push({
              expected: "boolean",
              code: "invalid_type",
              input: z,
              inst: A,
            }),
            K
          );
        }));
    })),
    (is6 = f8("$ZodBigInt", (A, q) => {
      (T3.init(A, q),
        (A._zod.pattern = gx1),
        (A._zod.parse = (K, Y) => {
          if (q.coerce)
            try {
              K.value = BigInt(K.value);
            } catch (z) {}
          if (typeof K.value === "bigint") return K;
          return (
            K.issues.push({
              expected: "bigint",
              code: "invalid_type",
              input: K.value,
              inst: A,
            }),
            K
          );
        }));
    })),
    (Fb1 = f8("$ZodBigInt", (A, q) => {
      (rx1.init(A, q), is6.init(A, q));
    })),
    (pb1 = f8("$ZodSymbol", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          let z = K.value;
          if (typeof z === "symbol") return K;
          return (
            K.issues.push({
              expected: "symbol",
              code: "invalid_type",
              input: z,
              inst: A,
            }),
            K
          );
        }));
    })),
    (Qb1 = f8("$ZodUndefined", (A, q) => {
      (T3.init(A, q),
        (A._zod.pattern = dx1),
        (A._zod.values = new Set([void 0])),
        (A._zod.optin = "optional"),
        (A._zod.optout = "optional"),
        (A._zod.parse = (K, Y) => {
          let z = K.value;
          if (typeof z > "u") return K;
          return (
            K.issues.push({
              expected: "undefined",
              code: "invalid_type",
              input: z,
              inst: A,
            }),
            K
          );
        }));
    })),
    (Ub1 = f8("$ZodNull", (A, q) => {
      (T3.init(A, q),
        (A._zod.pattern = Ux1),
        (A._zod.values = new Set([null])),
        (A._zod.parse = (K, Y) => {
          let z = K.value;
          if (z === null) return K;
          return (
            K.issues.push({
              expected: "null",
              code: "invalid_type",
              input: z,
              inst: A,
            }),
            K
          );
        }));
    })),
    (db1 = f8("$ZodAny", (A, q) => {
      (T3.init(A, q), (A._zod.parse = (K) => K));
    })),
    (O$6 = f8("$ZodUnknown", (A, q) => {
      (T3.init(A, q), (A._zod.parse = (K) => K));
    })),
    (cb1 = f8("$ZodNever", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          return (
            K.issues.push({
              expected: "never",
              code: "invalid_type",
              input: K.value,
              inst: A,
            }),
            K
          );
        }));
    })),
    (lb1 = f8("$ZodVoid", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          let z = K.value;
          if (typeof z > "u") return K;
          return (
            K.issues.push({
              expected: "void",
              code: "invalid_type",
              input: z,
              inst: A,
            }),
            K
          );
        }));
    })),
    (ib1 = f8("$ZodDate", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          if (q.coerce)
            try {
              K.value = new Date(K.value);
            } catch ($) {}
          let z = K.value,
            w = z instanceof Date;
          if (w && !Number.isNaN(z.getTime())) return K;
          return (
            K.issues.push({
              expected: "date",
              code: "invalid_type",
              input: z,
              ...(w ? { received: "Invalid Date" } : {}),
              inst: A,
            }),
            K
          );
        }));
    })));
  HE6 = f8("$ZodArray", (A, q) => {
    (T3.init(A, q),
      (A._zod.parse = (K, Y) => {
        let z = K.value;
        if (!Array.isArray(z))
          return (
            K.issues.push({
              expected: "array",
              code: "invalid_type",
              input: z,
              inst: A,
            }),
            K
          );
        K.value = Array(z.length);
        let w = [];
        for (let _ = 0; _ < z.length; _++) {
          let $ = z[_],
            O = q.element._zod.run({ value: $, issues: [] }, Y);
          if (O instanceof Promise) w.push(O.then((H) => he8(H, K, _)));
          else he8(O, K, _);
        }
        if (w.length) return Promise.all(w).then(() => K);
        return K;
      }));
  });
  nb1 = f8("$ZodObject", (A, q) => {
    T3.init(A, q);
    let K = sk6(() => {
      let J = Object.keys(q.shape);
      for (let X of J)
        if (!(q.shape[X] instanceof T3))
          throw Error(`Invalid element at key "${X}": expected a Zod schema`);
      let D = jx1(q.shape);
      return {
        shape: q.shape,
        keys: J,
        keySet: new Set(J),
        numKeys: J.length,
        optionalKeys: new Set(D),
      };
    });
    Uz(A._zod, "propValues", () => {
      let J = q.shape,
        D = {};
      for (let X in J) {
        let M = J[X]._zod;
        if (M.values) {
          D[X] ?? (D[X] = new Set());
          for (let P of M.values) D[X].add(P);
        }
      }
      return D;
    });
    let Y = (J) => {
        let D = new Us6(["shape", "payload", "ctx"]),
          X = K.value,
          M = (Z) => {
            let f = lA6(Z);
            return `shape[${f}]._zod.run({ value: input[${f}], issues: [] }, ctx)`;
          };
        D.write("const input = payload.value;");
        let P = Object.create(null),
          W = 0;
        for (let Z of X.keys) P[Z] = `key_${W++}`;
        D.write("const newResult = {}");
        for (let Z of X.keys)
          if (X.optionalKeys.has(Z)) {
            let f = P[Z];
            D.write(`const ${f} = ${M(Z)};`);
            let N = lA6(Z);
            D.write(`
        if (${f}.issues.length) {
          if (input[${N}] === undefined) {
            if (${N} in input) {
              newResult[${N}] = undefined;
            }
          } else {
            payload.issues = payload.issues.concat(
              ${f}.issues.map((iss) => ({
                ...iss,
                path: iss.path ? [${N}, ...iss.path] : [${N}],
              }))
            );
          }
        } else if (${f}.value === undefined) {
          if (${N} in input) newResult[${N}] = undefined;
        } else {
          newResult[${N}] = ${f}.value;
        }
        `);
          } else {
            let f = P[Z];
            (D.write(`const ${f} = ${M(Z)};`),
              D.write(`
          if (${f}.issues.length) payload.issues = payload.issues.concat(${f}.issues.map(iss => ({
            ...iss,
            path: iss.path ? [${lA6(Z)}, ...iss.path] : [${lA6(Z)}]
          })));`),
              D.write(`newResult[${lA6(Z)}] = ${f}.value`));
          }
        (D.write("payload.value = newResult;"), D.write("return payload;"));
        let G = D.compile();
        return (Z, f) => G(J, Z, f);
      },
      z,
      w = Y$6,
      _ = !nk6.jitless,
      O = _ && Ox1.value,
      H = q.catchall,
      j;
    A._zod.parse = (J, D) => {
      j ?? (j = K.value);
      let X = J.value;
      if (!w(X))
        return (
          J.issues.push({
            expected: "object",
            code: "invalid_type",
            input: X,
            inst: A,
          }),
          J
        );
      let M = [];
      if (_ && O && D?.async === !1 && D.jitless !== !0) {
        if (!z) z = Y(q.shape);
        J = z(J, D);
      } else {
        J.value = {};
        let f = j.shape;
        for (let N of j.keys) {
          let V = f[N],
            v = V._zod.run({ value: X[N], issues: [] }, D),
            L = V._zod.optin === "optional" && V._zod.optout === "optional";
          if (v instanceof Promise)
            M.push(v.then((S) => (L ? Ie8(S, J, N, X) : ds6(S, J, N))));
          else if (L) Ie8(v, J, N, X);
          else ds6(v, J, N);
        }
      }
      if (!H) return M.length ? Promise.all(M).then(() => J) : J;
      let P = [],
        W = j.keySet,
        G = H._zod,
        Z = G.def.type;
      for (let f of Object.keys(X)) {
        if (W.has(f)) continue;
        if (Z === "never") {
          P.push(f);
          continue;
        }
        let N = G.run({ value: X[f], issues: [] }, D);
        if (N instanceof Promise) M.push(N.then((V) => ds6(V, J, f)));
        else ds6(N, J, f);
      }
      if (P.length)
        J.issues.push({
          code: "unrecognized_keys",
          keys: P,
          input: X,
          inst: A,
        });
      if (!M.length) return J;
      return Promise.all(M).then(() => {
        return J;
      });
    };
  });
  ((ns6 = f8("$ZodUnion", (A, q) => {
    (T3.init(A, q),
      Uz(A._zod, "optin", () =>
        q.options.some((K) => K._zod.optin === "optional")
          ? "optional"
          : void 0,
      ),
      Uz(A._zod, "optout", () =>
        q.options.some((K) => K._zod.optout === "optional")
          ? "optional"
          : void 0,
      ),
      Uz(A._zod, "values", () => {
        if (q.options.every((K) => K._zod.values))
          return new Set(q.options.flatMap((K) => Array.from(K._zod.values)));
        return;
      }),
      Uz(A._zod, "pattern", () => {
        if (q.options.every((K) => K._zod.pattern)) {
          let K = q.options.map((Y) => Y._zod.pattern);
          return new RegExp(`^(${K.map((Y) => tk6(Y.source)).join("|")})$`);
        }
        return;
      }),
      (A._zod.parse = (K, Y) => {
        let z = !1,
          w = [];
        for (let _ of q.options) {
          let $ = _._zod.run({ value: K.value, issues: [] }, Y);
          if ($ instanceof Promise) (w.push($), (z = !0));
          else {
            if ($.issues.length === 0) return $;
            w.push($);
          }
        }
        if (!z) return xe8(w, K, A, Y);
        return Promise.all(w).then((_) => {
          return xe8(_, K, A, Y);
        });
      }));
  })),
    (rb1 = f8("$ZodDiscriminatedUnion", (A, q) => {
      ns6.init(A, q);
      let K = A._zod.parse;
      Uz(A._zod, "propValues", () => {
        let z = {};
        for (let w of q.options) {
          let _ = w._zod.propValues;
          if (!_ || Object.keys(_).length === 0)
            throw Error(
              `Invalid discriminated union option at index "${q.options.indexOf(w)}"`,
            );
          for (let [$, O] of Object.entries(_)) {
            if (!z[$]) z[$] = new Set();
            for (let H of O) z[$].add(H);
          }
        }
        return z;
      });
      let Y = sk6(() => {
        let z = q.options,
          w = new Map();
        for (let _ of z) {
          let $ = _._zod.propValues[q.discriminator];
          if (!$ || $.size === 0)
            throw Error(
              `Invalid discriminated union option at index "${q.options.indexOf(_)}"`,
            );
          for (let O of $) {
            if (w.has(O))
              throw Error(`Duplicate discriminator value "${String(O)}"`);
            w.set(O, _);
          }
        }
        return w;
      });
      A._zod.parse = (z, w) => {
        let _ = z.value;
        if (!Y$6(_))
          return (
            z.issues.push({
              code: "invalid_type",
              expected: "object",
              input: _,
              inst: A,
            }),
            z
          );
        let $ = Y.value.get(_?.[q.discriminator]);
        if ($) return $._zod.run(z, w);
        if (q.unionFallback) return K(z, w);
        return (
          z.issues.push({
            code: "invalid_union",
            errors: [],
            note: "No matching discriminator",
            input: _,
            path: [q.discriminator],
            inst: A,
          }),
          z
        );
      };
    })),
    (ob1 = f8("$ZodIntersection", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          let z = K.value,
            w = q.left._zod.run({ value: z, issues: [] }, Y),
            _ = q.right._zod.run({ value: z, issues: [] }, Y);
          if (w instanceof Promise || _ instanceof Promise)
            return Promise.all([w, _]).then(([O, H]) => {
              return be8(K, O, H);
            });
          return be8(K, w, _);
        }));
    })));
  aA6 = f8("$ZodTuple", (A, q) => {
    T3.init(A, q);
    let K = q.items,
      Y =
        K.length -
        [...K].reverse().findIndex((z) => z._zod.optin !== "optional");
    A._zod.parse = (z, w) => {
      let _ = z.value;
      if (!Array.isArray(_))
        return (
          z.issues.push({
            input: _,
            inst: A,
            expected: "tuple",
            code: "invalid_type",
          }),
          z
        );
      z.value = [];
      let $ = [];
      if (!q.rest) {
        let H = _.length > K.length,
          j = _.length < Y - 1;
        if (H || j)
          return (
            z.issues.push({
              input: _,
              inst: A,
              origin: "array",
              ...(H
                ? { code: "too_big", maximum: K.length }
                : { code: "too_small", minimum: K.length }),
            }),
            z
          );
      }
      let O = -1;
      for (let H of K) {
        if ((O++, O >= _.length)) {
          if (O >= Y) continue;
        }
        let j = H._zod.run({ value: _[O], issues: [] }, w);
        if (j instanceof Promise) $.push(j.then((J) => cs6(J, z, O)));
        else cs6(j, z, O);
      }
      if (q.rest) {
        let H = _.slice(K.length);
        for (let j of H) {
          O++;
          let J = q.rest._zod.run({ value: j, issues: [] }, w);
          if (J instanceof Promise) $.push(J.then((D) => cs6(D, z, O)));
          else cs6(J, z, O);
        }
      }
      if ($.length) return Promise.all($).then(() => z);
      return z;
    };
  });
  ((ab1 = f8("$ZodRecord", (A, q) => {
    (T3.init(A, q),
      (A._zod.parse = (K, Y) => {
        let z = K.value;
        if (!z$6(z))
          return (
            K.issues.push({
              expected: "record",
              code: "invalid_type",
              input: z,
              inst: A,
            }),
            K
          );
        let w = [];
        if (q.keyType._zod.values) {
          let _ = q.keyType._zod.values;
          K.value = {};
          for (let O of _)
            if (
              typeof O === "string" ||
              typeof O === "number" ||
              typeof O === "symbol"
            ) {
              let H = q.valueType._zod.run({ value: z[O], issues: [] }, Y);
              if (H instanceof Promise)
                w.push(
                  H.then((j) => {
                    if (j.issues.length) K.issues.push(...BT(O, j.issues));
                    K.value[O] = j.value;
                  }),
                );
              else {
                if (H.issues.length) K.issues.push(...BT(O, H.issues));
                K.value[O] = H.value;
              }
            }
          let $;
          for (let O in z) if (!_.has(O)) (($ = $ ?? []), $.push(O));
          if ($ && $.length > 0)
            K.issues.push({
              code: "unrecognized_keys",
              input: z,
              inst: A,
              keys: $,
            });
        } else {
          K.value = {};
          for (let _ of Reflect.ownKeys(z)) {
            if (_ === "__proto__") continue;
            let $ = q.keyType._zod.run({ value: _, issues: [] }, Y);
            if ($ instanceof Promise)
              throw Error(
                "Async schemas not supported in object keys currently",
              );
            if ($.issues.length) {
              (K.issues.push({
                origin: "record",
                code: "invalid_key",
                issues: $.issues.map((H) => vv(H, Y, bJ())),
                input: _,
                path: [_],
                inst: A,
              }),
                (K.value[$.value] = $.value));
              continue;
            }
            let O = q.valueType._zod.run({ value: z[_], issues: [] }, Y);
            if (O instanceof Promise)
              w.push(
                O.then((H) => {
                  if (H.issues.length) K.issues.push(...BT(_, H.issues));
                  K.value[$.value] = H.value;
                }),
              );
            else {
              if (O.issues.length) K.issues.push(...BT(_, O.issues));
              K.value[$.value] = O.value;
            }
          }
        }
        if (w.length) return Promise.all(w).then(() => K);
        return K;
      }));
  })),
    (sb1 = f8("$ZodMap", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          let z = K.value;
          if (!(z instanceof Map))
            return (
              K.issues.push({
                expected: "map",
                code: "invalid_type",
                input: z,
                inst: A,
              }),
              K
            );
          let w = [];
          K.value = new Map();
          for (let [_, $] of z) {
            let O = q.keyType._zod.run({ value: _, issues: [] }, Y),
              H = q.valueType._zod.run({ value: $, issues: [] }, Y);
            if (O instanceof Promise || H instanceof Promise)
              w.push(
                Promise.all([O, H]).then(([j, J]) => {
                  ue8(j, J, K, _, z, A, Y);
                }),
              );
            else ue8(O, H, K, _, z, A, Y);
          }
          if (w.length) return Promise.all(w).then(() => K);
          return K;
        }));
    })));
  tb1 = f8("$ZodSet", (A, q) => {
    (T3.init(A, q),
      (A._zod.parse = (K, Y) => {
        let z = K.value;
        if (!(z instanceof Set))
          return (
            K.issues.push({
              input: z,
              inst: A,
              expected: "set",
              code: "invalid_type",
            }),
            K
          );
        let w = [];
        K.value = new Set();
        for (let _ of z) {
          let $ = q.valueType._zod.run({ value: _, issues: [] }, Y);
          if ($ instanceof Promise) w.push($.then((O) => me8(O, K)));
          else me8($, K);
        }
        if (w.length) return Promise.all(w).then(() => K);
        return K;
      }));
  });
  ((eb1 = f8("$ZodEnum", (A, q) => {
    T3.init(A, q);
    let K = ak6(q.entries);
    ((A._zod.values = new Set(K)),
      (A._zod.pattern = new RegExp(
        `^(${K.filter((Y) => ek6.has(typeof Y))
          .map((Y) => (typeof Y === "string" ? AQ(Y) : Y.toString()))
          .join("|")})$`,
      )),
      (A._zod.parse = (Y, z) => {
        let w = Y.value;
        if (A._zod.values.has(w)) return Y;
        return (
          Y.issues.push({
            code: "invalid_value",
            values: K,
            input: w,
            inst: A,
          }),
          Y
        );
      }));
  })),
    (Au1 = f8("$ZodLiteral", (A, q) => {
      (T3.init(A, q),
        (A._zod.values = new Set(q.values)),
        (A._zod.pattern = new RegExp(
          `^(${q.values.map((K) => (typeof K === "string" ? AQ(K) : K ? K.toString() : String(K))).join("|")})$`,
        )),
        (A._zod.parse = (K, Y) => {
          let z = K.value;
          if (A._zod.values.has(z)) return K;
          return (
            K.issues.push({
              code: "invalid_value",
              values: q.values,
              input: z,
              inst: A,
            }),
            K
          );
        }));
    })),
    (qu1 = f8("$ZodFile", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          let z = K.value;
          if (z instanceof File) return K;
          return (
            K.issues.push({
              expected: "file",
              code: "invalid_type",
              input: z,
              inst: A,
            }),
            K
          );
        }));
    })),
    (jE6 = f8("$ZodTransform", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          let z = q.transform(K.value, K);
          if (Y.async)
            return (z instanceof Promise ? z : Promise.resolve(z)).then((_) => {
              return ((K.value = _), K);
            });
          if (z instanceof Promise) throw new ep();
          return ((K.value = z), K);
        }));
    })),
    (Ku1 = f8("$ZodOptional", (A, q) => {
      (T3.init(A, q),
        (A._zod.optin = "optional"),
        (A._zod.optout = "optional"),
        Uz(A._zod, "values", () => {
          return q.innerType._zod.values
            ? new Set([...q.innerType._zod.values, void 0])
            : void 0;
        }),
        Uz(A._zod, "pattern", () => {
          let K = q.innerType._zod.pattern;
          return K ? new RegExp(`^(${tk6(K.source)})?$`) : void 0;
        }),
        (A._zod.parse = (K, Y) => {
          if (q.innerType._zod.optin === "optional")
            return q.innerType._zod.run(K, Y);
          if (K.value === void 0) return K;
          return q.innerType._zod.run(K, Y);
        }));
    })),
    (Yu1 = f8("$ZodNullable", (A, q) => {
      (T3.init(A, q),
        Uz(A._zod, "optin", () => q.innerType._zod.optin),
        Uz(A._zod, "optout", () => q.innerType._zod.optout),
        Uz(A._zod, "pattern", () => {
          let K = q.innerType._zod.pattern;
          return K ? new RegExp(`^(${tk6(K.source)}|null)$`) : void 0;
        }),
        Uz(A._zod, "values", () => {
          return q.innerType._zod.values
            ? new Set([...q.innerType._zod.values, null])
            : void 0;
        }),
        (A._zod.parse = (K, Y) => {
          if (K.value === null) return K;
          return q.innerType._zod.run(K, Y);
        }));
    })),
    (zu1 = f8("$ZodDefault", (A, q) => {
      (T3.init(A, q),
        (A._zod.optin = "optional"),
        Uz(A._zod, "values", () => q.innerType._zod.values),
        (A._zod.parse = (K, Y) => {
          if (K.value === void 0) return ((K.value = q.defaultValue), K);
          let z = q.innerType._zod.run(K, Y);
          if (z instanceof Promise) return z.then((w) => Be8(w, q));
          return Be8(z, q);
        }));
    })));
  ((wu1 = f8("$ZodPrefault", (A, q) => {
    (T3.init(A, q),
      (A._zod.optin = "optional"),
      Uz(A._zod, "values", () => q.innerType._zod.values),
      (A._zod.parse = (K, Y) => {
        if (K.value === void 0) K.value = q.defaultValue;
        return q.innerType._zod.run(K, Y);
      }));
  })),
    (_u1 = f8("$ZodNonOptional", (A, q) => {
      (T3.init(A, q),
        Uz(A._zod, "values", () => {
          let K = q.innerType._zod.values;
          return K ? new Set([...K].filter((Y) => Y !== void 0)) : void 0;
        }),
        (A._zod.parse = (K, Y) => {
          let z = q.innerType._zod.run(K, Y);
          if (z instanceof Promise) return z.then((w) => ge8(w, A));
          return ge8(z, A);
        }));
    })));
  (($u1 = f8("$ZodSuccess", (A, q) => {
    (T3.init(A, q),
      (A._zod.parse = (K, Y) => {
        let z = q.innerType._zod.run(K, Y);
        if (z instanceof Promise)
          return z.then((w) => {
            return ((K.value = w.issues.length === 0), K);
          });
        return ((K.value = z.issues.length === 0), K);
      }));
  })),
    (Ou1 = f8("$ZodCatch", (A, q) => {
      (T3.init(A, q),
        (A._zod.optin = "optional"),
        Uz(A._zod, "optout", () => q.innerType._zod.optout),
        Uz(A._zod, "values", () => q.innerType._zod.values),
        (A._zod.parse = (K, Y) => {
          let z = q.innerType._zod.run(K, Y);
          if (z instanceof Promise)
            return z.then((w) => {
              if (((K.value = w.value), w.issues.length))
                ((K.value = q.catchValue({
                  ...K,
                  error: { issues: w.issues.map((_) => vv(_, Y, bJ())) },
                  input: K.value,
                })),
                  (K.issues = []));
              return K;
            });
          if (((K.value = z.value), z.issues.length))
            ((K.value = q.catchValue({
              ...K,
              error: { issues: z.issues.map((w) => vv(w, Y, bJ())) },
              input: K.value,
            })),
              (K.issues = []));
          return K;
        }));
    })),
    (Hu1 = f8("$ZodNaN", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          if (typeof K.value !== "number" || !Number.isNaN(K.value))
            return (
              K.issues.push({
                input: K.value,
                inst: A,
                expected: "nan",
                code: "invalid_type",
              }),
              K
            );
          return K;
        }));
    })),
    (JE6 = f8("$ZodPipe", (A, q) => {
      (T3.init(A, q),
        Uz(A._zod, "values", () => q.in._zod.values),
        Uz(A._zod, "optin", () => q.in._zod.optin),
        Uz(A._zod, "optout", () => q.out._zod.optout),
        (A._zod.parse = (K, Y) => {
          let z = q.in._zod.run(K, Y);
          if (z instanceof Promise) return z.then((w) => Fe8(w, q, Y));
          return Fe8(z, q, Y);
        }));
    })));
  ju1 = f8("$ZodReadonly", (A, q) => {
    (T3.init(A, q),
      Uz(A._zod, "propValues", () => q.innerType._zod.propValues),
      Uz(A._zod, "values", () => q.innerType._zod.values),
      Uz(A._zod, "optin", () => q.innerType._zod.optin),
      Uz(A._zod, "optout", () => q.innerType._zod.optout),
      (A._zod.parse = (K, Y) => {
        let z = q.innerType._zod.run(K, Y);
        if (z instanceof Promise) return z.then(pe8);
        return pe8(z);
      }));
  });
  ((Ju1 = f8("$ZodTemplateLiteral", (A, q) => {
    T3.init(A, q);
    let K = [];
    for (let Y of q.parts)
      if (Y instanceof T3) {
        if (!Y._zod.pattern)
          throw Error(
            `Invalid template literal part, no pattern found: ${[...Y._zod.traits].shift()}`,
          );
        let z =
          Y._zod.pattern instanceof RegExp
            ? Y._zod.pattern.source
            : Y._zod.pattern;
        if (!z) throw Error(`Invalid template literal part: ${Y._zod.traits}`);
        let w = z.startsWith("^") ? 1 : 0,
          _ = z.endsWith("$") ? z.length - 1 : z.length;
        K.push(z.slice(w, _));
      } else if (Y === null || Hx1.has(typeof Y)) K.push(AQ(`${Y}`));
      else throw Error(`Invalid template literal part: ${Y}`);
    ((A._zod.pattern = new RegExp(`^${K.join("")}$`)),
      (A._zod.parse = (Y, z) => {
        if (typeof Y.value !== "string")
          return (
            Y.issues.push({
              input: Y.value,
              inst: A,
              expected: "template_literal",
              code: "invalid_type",
            }),
            Y
          );
        if (((A._zod.pattern.lastIndex = 0), !A._zod.pattern.test(Y.value)))
          return (
            Y.issues.push({
              input: Y.value,
              inst: A,
              code: "invalid_format",
              format: "template_literal",
              pattern: A._zod.pattern.source,
            }),
            Y
          );
        return Y;
      }));
  })),
    (Du1 = f8("$ZodPromise", (A, q) => {
      (T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          return Promise.resolve(K.value).then((z) =>
            q.innerType._zod.run({ value: z, issues: [] }, Y),
          );
        }));
    })),
    (Xu1 = f8("$ZodLazy", (A, q) => {
      (T3.init(A, q),
        Uz(A._zod, "innerType", () => q.getter()),
        Uz(A._zod, "pattern", () => A._zod.innerType._zod.pattern),
        Uz(A._zod, "propValues", () => A._zod.innerType._zod.propValues),
        Uz(A._zod, "optin", () => A._zod.innerType._zod.optin),
        Uz(A._zod, "optout", () => A._zod.innerType._zod.optout),
        (A._zod.parse = (K, Y) => {
          return A._zod.innerType._zod.run(K, Y);
        }));
    })),
    (Mu1 = f8("$ZodCustom", (A, q) => {
      (cO.init(A, q),
        T3.init(A, q),
        (A._zod.parse = (K, Y) => {
          return K;
        }),
        (A._zod.check = (K) => {
          let Y = K.value,
            z = q.fn(Y);
          if (z instanceof Promise) return z.then((w) => Qe8(w, K, Y, A));
          Qe8(z, K, Y, A);
          return;
        }));
    })));
});
function Pu1() {
  return { localeError: _cq() };
}
var _cq = () => {
  let A = {
    string: { unit: "حرف", verb: "أن يحوي" },
    file: { unit: "بايت", verb: "أن يحوي" },
    array: { unit: "عنصر", verb: "أن يحوي" },
    set: { unit: "عنصر", verb: "أن يحوي" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "مدخل",
      email: "بريد إلكتروني",
      url: "رابط",
      emoji: "إيموجي",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "تاريخ ووقت بمعيار ISO",
      date: "تاريخ بمعيار ISO",
      time: "وقت بمعيار ISO",
      duration: "مدة بمعيار ISO",
      ipv4: "عنوان IPv4",
      ipv6: "عنوان IPv6",
      cidrv4: "مدى عناوين بصيغة IPv4",
      cidrv6: "مدى عناوين بصيغة IPv6",
      base64: "نَص بترميز base64-encoded",
      base64url: "نَص بترميز base64url-encoded",
      json_string: "نَص على هيئة JSON",
      e164: "رقم هاتف بمعيار E.164",
      jwt: "JWT",
      template_literal: "مدخل",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `مدخلات غير مقبولة: يفترض إدخال ${z.expected}، ولكن تم إدخال ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `مدخلات غير مقبولة: يفترض إدخال ${g7(z.values[0])}`;
        return `اختيار غير مقبول: يتوقع انتقاء أحد هذه الخيارات: ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return ` أكبر من اللازم: يفترض أن تكون ${z.origin ?? "القيمة"} ${w} ${z.maximum.toString()} ${_.unit ?? "عنصر"}`;
        return `أكبر من اللازم: يفترض أن تكون ${z.origin ?? "القيمة"} ${w} ${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `أصغر من اللازم: يفترض لـ ${z.origin} أن يكون ${w} ${z.minimum.toString()} ${_.unit}`;
        return `أصغر من اللازم: يفترض لـ ${z.origin} أن يكون ${w} ${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `نَص غير مقبول: يجب أن يبدأ بـ "${z.prefix}"`;
        if (w.format === "ends_with")
          return `نَص غير مقبول: يجب أن ينتهي بـ "${w.suffix}"`;
        if (w.format === "includes")
          return `نَص غير مقبول: يجب أن يتضمَّن "${w.includes}"`;
        if (w.format === "regex")
          return `نَص غير مقبول: يجب أن يطابق النمط ${w.pattern}`;
        return `${Y[w.format] ?? z.format} غير مقبول`;
      }
      case "not_multiple_of":
        return `رقم غير مقبول: يجب أن يكون من مضاعفات ${z.divisor}`;
      case "unrecognized_keys":
        return `معرف${z.keys.length > 1 ? "ات" : ""} غريب${z.keys.length > 1 ? "ة" : ""}: ${XA(z.keys, "، ")}`;
      case "invalid_key":
        return `معرف غير مقبول في ${z.origin}`;
      case "invalid_union":
        return "مدخل غير مقبول";
      case "invalid_element":
        return `مدخل غير مقبول في ${z.origin}`;
      default:
        return "مدخل غير مقبول";
    }
  };
};
var le8 = E(() => {
  A3();
});
function Wu1() {
  return { localeError: $cq() };
}
var $cq = () => {
  let A = {
    string: { unit: "simvol", verb: "olmalıdır" },
    file: { unit: "bayt", verb: "olmalıdır" },
    array: { unit: "element", verb: "olmalıdır" },
    set: { unit: "element", verb: "olmalıdır" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "input",
      email: "email address",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO datetime",
      date: "ISO date",
      time: "ISO time",
      duration: "ISO duration",
      ipv4: "IPv4 address",
      ipv6: "IPv6 address",
      cidrv4: "IPv4 range",
      cidrv6: "IPv6 range",
      base64: "base64-encoded string",
      base64url: "base64url-encoded string",
      json_string: "JSON string",
      e164: "E.164 number",
      jwt: "JWT",
      template_literal: "input",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Yanlış dəyər: gözlənilən ${z.expected}, daxil olan ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Yanlış dəyər: gözlənilən ${g7(z.values[0])}`;
        return `Yanlış seçim: aşağıdakılardan biri olmalıdır: ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Çox böyük: gözlənilən ${z.origin ?? "dəyər"} ${w}${z.maximum.toString()} ${_.unit ?? "element"}`;
        return `Çox böyük: gözlənilən ${z.origin ?? "dəyər"} ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Çox kiçik: gözlənilən ${z.origin} ${w}${z.minimum.toString()} ${_.unit}`;
        return `Çox kiçik: gözlənilən ${z.origin} ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Yanlış mətn: "${w.prefix}" ilə başlamalıdır`;
        if (w.format === "ends_with")
          return `Yanlış mətn: "${w.suffix}" ilə bitməlidir`;
        if (w.format === "includes")
          return `Yanlış mətn: "${w.includes}" daxil olmalıdır`;
        if (w.format === "regex")
          return `Yanlış mətn: ${w.pattern} şablonuna uyğun olmalıdır`;
        return `Yanlış ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Yanlış ədəd: ${z.divisor} ilə bölünə bilən olmalıdır`;
      case "unrecognized_keys":
        return `Tanınmayan açar${z.keys.length > 1 ? "lar" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `${z.origin} daxilində yanlış açar`;
      case "invalid_union":
        return "Yanlış dəyər";
      case "invalid_element":
        return `${z.origin} daxilində yanlış dəyər`;
      default:
        return "Yanlış dəyər";
    }
  };
};
var ie8 = E(() => {
  A3();
});
function ne8(A, q, K, Y) {
  let z = Math.abs(A),
    w = z % 10,
    _ = z % 100;
  if (_ >= 11 && _ <= 19) return Y;
  if (w === 1) return q;
  if (w >= 2 && w <= 4) return K;
  return Y;
}
function Gu1() {
  return { localeError: Ocq() };
}
var Ocq = () => {
  let A = {
    string: {
      unit: { one: "сімвал", few: "сімвалы", many: "сімвалаў" },
      verb: "мець",
    },
    array: {
      unit: { one: "элемент", few: "элементы", many: "элементаў" },
      verb: "мець",
    },
    set: {
      unit: { one: "элемент", few: "элементы", many: "элементаў" },
      verb: "мець",
    },
    file: { unit: { one: "байт", few: "байты", many: "байтаў" }, verb: "мець" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "лік";
        case "object": {
          if (Array.isArray(z)) return "масіў";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "увод",
      email: "email адрас",
      url: "URL",
      emoji: "эмодзі",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO дата і час",
      date: "ISO дата",
      time: "ISO час",
      duration: "ISO працягласць",
      ipv4: "IPv4 адрас",
      ipv6: "IPv6 адрас",
      cidrv4: "IPv4 дыяпазон",
      cidrv6: "IPv6 дыяпазон",
      base64: "радок у фармаце base64",
      base64url: "радок у фармаце base64url",
      json_string: "JSON радок",
      e164: "нумар E.164",
      jwt: "JWT",
      template_literal: "увод",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Няправільны ўвод: чакаўся ${z.expected}, атрымана ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Няправільны ўвод: чакалася ${g7(z.values[0])}`;
        return `Няправільны варыянт: чакаўся адзін з ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_) {
          let $ = Number(z.maximum),
            O = ne8($, _.unit.one, _.unit.few, _.unit.many);
          return `Занадта вялікі: чакалася, што ${z.origin ?? "значэнне"} павінна ${_.verb} ${w}${z.maximum.toString()} ${O}`;
        }
        return `Занадта вялікі: чакалася, што ${z.origin ?? "значэнне"} павінна быць ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_) {
          let $ = Number(z.minimum),
            O = ne8($, _.unit.one, _.unit.few, _.unit.many);
          return `Занадта малы: чакалася, што ${z.origin} павінна ${_.verb} ${w}${z.minimum.toString()} ${O}`;
        }
        return `Занадта малы: чакалася, што ${z.origin} павінна быць ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Няправільны радок: павінен пачынацца з "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Няправільны радок: павінен заканчвацца на "${w.suffix}"`;
        if (w.format === "includes")
          return `Няправільны радок: павінен змяшчаць "${w.includes}"`;
        if (w.format === "regex")
          return `Няправільны радок: павінен адпавядаць шаблону ${w.pattern}`;
        return `Няправільны ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Няправільны лік: павінен быць кратным ${z.divisor}`;
      case "unrecognized_keys":
        return `Нераспазнаны ${z.keys.length > 1 ? "ключы" : "ключ"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Няправільны ключ у ${z.origin}`;
      case "invalid_union":
        return "Няправільны ўвод";
      case "invalid_element":
        return `Няправільнае значэнне ў ${z.origin}`;
      default:
        return "Няправільны ўвод";
    }
  };
};
var re8 = E(() => {
  A3();
});
function Zu1() {
  return { localeError: Hcq() };
}
var Hcq = () => {
  let A = {
    string: { unit: "caràcters", verb: "contenir" },
    file: { unit: "bytes", verb: "contenir" },
    array: { unit: "elements", verb: "contenir" },
    set: { unit: "elements", verb: "contenir" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "entrada",
      email: "adreça electrònica",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "data i hora ISO",
      date: "data ISO",
      time: "hora ISO",
      duration: "durada ISO",
      ipv4: "adreça IPv4",
      ipv6: "adreça IPv6",
      cidrv4: "rang IPv4",
      cidrv6: "rang IPv6",
      base64: "cadena codificada en base64",
      base64url: "cadena codificada en base64url",
      json_string: "cadena JSON",
      e164: "número E.164",
      jwt: "JWT",
      template_literal: "entrada",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Tipus invàlid: s'esperava ${z.expected}, s'ha rebut ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Valor invàlid: s'esperava ${g7(z.values[0])}`;
        return `Opció invàlida: s'esperava una de ${XA(z.values, " o ")}`;
      case "too_big": {
        let w = z.inclusive ? "com a màxim" : "menys de",
          _ = q(z.origin);
        if (_)
          return `Massa gran: s'esperava que ${z.origin ?? "el valor"} contingués ${w} ${z.maximum.toString()} ${_.unit ?? "elements"}`;
        return `Massa gran: s'esperava que ${z.origin ?? "el valor"} fos ${w} ${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? "com a mínim" : "més de",
          _ = q(z.origin);
        if (_)
          return `Massa petit: s'esperava que ${z.origin} contingués ${w} ${z.minimum.toString()} ${_.unit}`;
        return `Massa petit: s'esperava que ${z.origin} fos ${w} ${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Format invàlid: ha de començar amb "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Format invàlid: ha d'acabar amb "${w.suffix}"`;
        if (w.format === "includes")
          return `Format invàlid: ha d'incloure "${w.includes}"`;
        if (w.format === "regex")
          return `Format invàlid: ha de coincidir amb el patró ${w.pattern}`;
        return `Format invàlid per a ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Número invàlid: ha de ser múltiple de ${z.divisor}`;
      case "unrecognized_keys":
        return `Clau${z.keys.length > 1 ? "s" : ""} no reconeguda${z.keys.length > 1 ? "s" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Clau invàlida a ${z.origin}`;
      case "invalid_union":
        return "Entrada invàlida";
      case "invalid_element":
        return `Element invàlid a ${z.origin}`;
      default:
        return "Entrada invàlida";
    }
  };
};
var oe8 = E(() => {
  A3();
});
function fu1() {
  return { localeError: jcq() };
}
var jcq = () => {
  let A = {
    string: { unit: "znaků", verb: "mít" },
    file: { unit: "bajtů", verb: "mít" },
    array: { unit: "prvků", verb: "mít" },
    set: { unit: "prvků", verb: "mít" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "číslo";
        case "string":
          return "řetězec";
        case "boolean":
          return "boolean";
        case "bigint":
          return "bigint";
        case "function":
          return "funkce";
        case "symbol":
          return "symbol";
        case "undefined":
          return "undefined";
        case "object": {
          if (Array.isArray(z)) return "pole";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "regulární výraz",
      email: "e-mailová adresa",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "datum a čas ve formátu ISO",
      date: "datum ve formátu ISO",
      time: "čas ve formátu ISO",
      duration: "doba trvání ISO",
      ipv4: "IPv4 adresa",
      ipv6: "IPv6 adresa",
      cidrv4: "rozsah IPv4",
      cidrv6: "rozsah IPv6",
      base64: "řetězec zakódovaný ve formátu base64",
      base64url: "řetězec zakódovaný ve formátu base64url",
      json_string: "řetězec ve formátu JSON",
      e164: "číslo E.164",
      jwt: "JWT",
      template_literal: "vstup",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Neplatný vstup: očekáváno ${z.expected}, obdrženo ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Neplatný vstup: očekáváno ${g7(z.values[0])}`;
        return `Neplatná možnost: očekávána jedna z hodnot ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Hodnota je příliš velká: ${z.origin ?? "hodnota"} musí mít ${w}${z.maximum.toString()} ${_.unit ?? "prvků"}`;
        return `Hodnota je příliš velká: ${z.origin ?? "hodnota"} musí být ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Hodnota je příliš malá: ${z.origin ?? "hodnota"} musí mít ${w}${z.minimum.toString()} ${_.unit ?? "prvků"}`;
        return `Hodnota je příliš malá: ${z.origin ?? "hodnota"} musí být ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Neplatný řetězec: musí začínat na "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Neplatný řetězec: musí končit na "${w.suffix}"`;
        if (w.format === "includes")
          return `Neplatný řetězec: musí obsahovat "${w.includes}"`;
        if (w.format === "regex")
          return `Neplatný řetězec: musí odpovídat vzoru ${w.pattern}`;
        return `Neplatný formát ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Neplatné číslo: musí být násobkem ${z.divisor}`;
      case "unrecognized_keys":
        return `Neznámé klíče: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Neplatný klíč v ${z.origin}`;
      case "invalid_union":
        return "Neplatný vstup";
      case "invalid_element":
        return `Neplatná hodnota v ${z.origin}`;
      default:
        return "Neplatný vstup";
    }
  };
};
var ae8 = E(() => {
  A3();
});
function Tu1() {
  return { localeError: Jcq() };
}
var Jcq = () => {
  let A = {
    string: { unit: "Zeichen", verb: "zu haben" },
    file: { unit: "Bytes", verb: "zu haben" },
    array: { unit: "Elemente", verb: "zu haben" },
    set: { unit: "Elemente", verb: "zu haben" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "Zahl";
        case "object": {
          if (Array.isArray(z)) return "Array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "Eingabe",
      email: "E-Mail-Adresse",
      url: "URL",
      emoji: "Emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO-Datum und -Uhrzeit",
      date: "ISO-Datum",
      time: "ISO-Uhrzeit",
      duration: "ISO-Dauer",
      ipv4: "IPv4-Adresse",
      ipv6: "IPv6-Adresse",
      cidrv4: "IPv4-Bereich",
      cidrv6: "IPv6-Bereich",
      base64: "Base64-codierter String",
      base64url: "Base64-URL-codierter String",
      json_string: "JSON-String",
      e164: "E.164-Nummer",
      jwt: "JWT",
      template_literal: "Eingabe",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Ungültige Eingabe: erwartet ${z.expected}, erhalten ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Ungültige Eingabe: erwartet ${g7(z.values[0])}`;
        return `Ungültige Option: erwartet eine von ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Zu groß: erwartet, dass ${z.origin ?? "Wert"} ${w}${z.maximum.toString()} ${_.unit ?? "Elemente"} hat`;
        return `Zu groß: erwartet, dass ${z.origin ?? "Wert"} ${w}${z.maximum.toString()} ist`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Zu klein: erwartet, dass ${z.origin} ${w}${z.minimum.toString()} ${_.unit} hat`;
        return `Zu klein: erwartet, dass ${z.origin} ${w}${z.minimum.toString()} ist`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Ungültiger String: muss mit "${w.prefix}" beginnen`;
        if (w.format === "ends_with")
          return `Ungültiger String: muss mit "${w.suffix}" enden`;
        if (w.format === "includes")
          return `Ungültiger String: muss "${w.includes}" enthalten`;
        if (w.format === "regex")
          return `Ungültiger String: muss dem Muster ${w.pattern} entsprechen`;
        return `Ungültig: ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Ungültige Zahl: muss ein Vielfaches von ${z.divisor} sein`;
      case "unrecognized_keys":
        return `${z.keys.length > 1 ? "Unbekannte Schlüssel" : "Unbekannter Schlüssel"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Ungültiger Schlüssel in ${z.origin}`;
      case "invalid_union":
        return "Ungültige Eingabe";
      case "invalid_element":
        return `Ungültiger Wert in ${z.origin}`;
      default:
        return "Ungültige Eingabe";
    }
  };
};
var se8 = E(() => {
  A3();
});
function XE6() {
  return { localeError: Xcq() };
}
var Dcq = (A) => {
    let q = typeof A;
    switch (q) {
      case "number":
        return Number.isNaN(A) ? "NaN" : "number";
      case "object": {
        if (Array.isArray(A)) return "array";
        if (A === null) return "null";
        if (Object.getPrototypeOf(A) !== Object.prototype && A.constructor)
          return A.constructor.name;
      }
    }
    return q;
  },
  Xcq = () => {
    let A = {
      string: { unit: "characters", verb: "to have" },
      file: { unit: "bytes", verb: "to have" },
      array: { unit: "items", verb: "to have" },
      set: { unit: "items", verb: "to have" },
    };
    function q(Y) {
      return A[Y] ?? null;
    }
    let K = {
      regex: "input",
      email: "email address",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO datetime",
      date: "ISO date",
      time: "ISO time",
      duration: "ISO duration",
      ipv4: "IPv4 address",
      ipv6: "IPv6 address",
      cidrv4: "IPv4 range",
      cidrv6: "IPv6 range",
      base64: "base64-encoded string",
      base64url: "base64url-encoded string",
      json_string: "JSON string",
      e164: "E.164 number",
      jwt: "JWT",
      template_literal: "input",
    };
    return (Y) => {
      switch (Y.code) {
        case "invalid_type":
          return `Invalid input: expected ${Y.expected}, received ${Dcq(Y.input)}`;
        case "invalid_value":
          if (Y.values.length === 1)
            return `Invalid input: expected ${g7(Y.values[0])}`;
          return `Invalid option: expected one of ${XA(Y.values, "|")}`;
        case "too_big": {
          let z = Y.inclusive ? "<=" : "<",
            w = q(Y.origin);
          if (w)
            return `Too big: expected ${Y.origin ?? "value"} to have ${z}${Y.maximum.toString()} ${w.unit ?? "elements"}`;
          return `Too big: expected ${Y.origin ?? "value"} to be ${z}${Y.maximum.toString()}`;
        }
        case "too_small": {
          let z = Y.inclusive ? ">=" : ">",
            w = q(Y.origin);
          if (w)
            return `Too small: expected ${Y.origin} to have ${z}${Y.minimum.toString()} ${w.unit}`;
          return `Too small: expected ${Y.origin} to be ${z}${Y.minimum.toString()}`;
        }
        case "invalid_format": {
          let z = Y;
          if (z.format === "starts_with")
            return `Invalid string: must start with "${z.prefix}"`;
          if (z.format === "ends_with")
            return `Invalid string: must end with "${z.suffix}"`;
          if (z.format === "includes")
            return `Invalid string: must include "${z.includes}"`;
          if (z.format === "regex")
            return `Invalid string: must match pattern ${z.pattern}`;
          return `Invalid ${K[z.format] ?? Y.format}`;
        }
        case "not_multiple_of":
          return `Invalid number: must be a multiple of ${Y.divisor}`;
        case "unrecognized_keys":
          return `Unrecognized key${Y.keys.length > 1 ? "s" : ""}: ${XA(Y.keys, ", ")}`;
        case "invalid_key":
          return `Invalid key in ${Y.origin}`;
        case "invalid_union":
          return "Invalid input";
        case "invalid_element":
          return `Invalid value in ${Y.origin}`;
        default:
          return "Invalid input";
      }
    };
  };
var Nu1 = E(() => {
  A3();
});
function Vu1() {
  return { localeError: Pcq() };
}
var Mcq = (A) => {
    let q = typeof A;
    switch (q) {
      case "number":
        return Number.isNaN(A) ? "NaN" : "nombro";
      case "object": {
        if (Array.isArray(A)) return "tabelo";
        if (A === null) return "senvalora";
        if (Object.getPrototypeOf(A) !== Object.prototype && A.constructor)
          return A.constructor.name;
      }
    }
    return q;
  },
  Pcq = () => {
    let A = {
      string: { unit: "karaktrojn", verb: "havi" },
      file: { unit: "bajtojn", verb: "havi" },
      array: { unit: "elementojn", verb: "havi" },
      set: { unit: "elementojn", verb: "havi" },
    };
    function q(Y) {
      return A[Y] ?? null;
    }
    let K = {
      regex: "enigo",
      email: "retadreso",
      url: "URL",
      emoji: "emoĝio",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO-datotempo",
      date: "ISO-dato",
      time: "ISO-tempo",
      duration: "ISO-daŭro",
      ipv4: "IPv4-adreso",
      ipv6: "IPv6-adreso",
      cidrv4: "IPv4-rango",
      cidrv6: "IPv6-rango",
      base64: "64-ume kodita karaktraro",
      base64url: "URL-64-ume kodita karaktraro",
      json_string: "JSON-karaktraro",
      e164: "E.164-nombro",
      jwt: "JWT",
      template_literal: "enigo",
    };
    return (Y) => {
      switch (Y.code) {
        case "invalid_type":
          return `Nevalida enigo: atendiĝis ${Y.expected}, riceviĝis ${Mcq(Y.input)}`;
        case "invalid_value":
          if (Y.values.length === 1)
            return `Nevalida enigo: atendiĝis ${g7(Y.values[0])}`;
          return `Nevalida opcio: atendiĝis unu el ${XA(Y.values, "|")}`;
        case "too_big": {
          let z = Y.inclusive ? "<=" : "<",
            w = q(Y.origin);
          if (w)
            return `Tro granda: atendiĝis ke ${Y.origin ?? "valoro"} havu ${z}${Y.maximum.toString()} ${w.unit ?? "elementojn"}`;
          return `Tro granda: atendiĝis ke ${Y.origin ?? "valoro"} havu ${z}${Y.maximum.toString()}`;
        }
        case "too_small": {
          let z = Y.inclusive ? ">=" : ">",
            w = q(Y.origin);
          if (w)
            return `Tro malgranda: atendiĝis ke ${Y.origin} havu ${z}${Y.minimum.toString()} ${w.unit}`;
          return `Tro malgranda: atendiĝis ke ${Y.origin} estu ${z}${Y.minimum.toString()}`;
        }
        case "invalid_format": {
          let z = Y;
          if (z.format === "starts_with")
            return `Nevalida karaktraro: devas komenciĝi per "${z.prefix}"`;
          if (z.format === "ends_with")
            return `Nevalida karaktraro: devas finiĝi per "${z.suffix}"`;
          if (z.format === "includes")
            return `Nevalida karaktraro: devas inkluzivi "${z.includes}"`;
          if (z.format === "regex")
            return `Nevalida karaktraro: devas kongrui kun la modelo ${z.pattern}`;
          return `Nevalida ${K[z.format] ?? Y.format}`;
        }
        case "not_multiple_of":
          return `Nevalida nombro: devas esti oblo de ${Y.divisor}`;
        case "unrecognized_keys":
          return `Nekonata${Y.keys.length > 1 ? "j" : ""} ŝlosilo${Y.keys.length > 1 ? "j" : ""}: ${XA(Y.keys, ", ")}`;
        case "invalid_key":
          return `Nevalida ŝlosilo en ${Y.origin}`;
        case "invalid_union":
          return "Nevalida enigo";
        case "invalid_element":
          return `Nevalida valoro en ${Y.origin}`;
        default:
          return "Nevalida enigo";
      }
    };
  };
var te8 = E(() => {
  A3();
});
function vu1() {
  return { localeError: Wcq() };
}
var Wcq = () => {
  let A = {
    string: { unit: "caracteres", verb: "tener" },
    file: { unit: "bytes", verb: "tener" },
    array: { unit: "elementos", verb: "tener" },
    set: { unit: "elementos", verb: "tener" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "número";
        case "object": {
          if (Array.isArray(z)) return "arreglo";
          if (z === null) return "nulo";
          if (Object.getPrototypeOf(z) !== Object.prototype)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "entrada",
      email: "dirección de correo electrónico",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "fecha y hora ISO",
      date: "fecha ISO",
      time: "hora ISO",
      duration: "duración ISO",
      ipv4: "dirección IPv4",
      ipv6: "dirección IPv6",
      cidrv4: "rango IPv4",
      cidrv6: "rango IPv6",
      base64: "cadena codificada en base64",
      base64url: "URL codificada en base64",
      json_string: "cadena JSON",
      e164: "número E.164",
      jwt: "JWT",
      template_literal: "entrada",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Entrada inválida: se esperaba ${z.expected}, recibido ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Entrada inválida: se esperaba ${g7(z.values[0])}`;
        return `Opción inválida: se esperaba una de ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Demasiado grande: se esperaba que ${z.origin ?? "valor"} tuviera ${w}${z.maximum.toString()} ${_.unit ?? "elementos"}`;
        return `Demasiado grande: se esperaba que ${z.origin ?? "valor"} fuera ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Demasiado pequeño: se esperaba que ${z.origin} tuviera ${w}${z.minimum.toString()} ${_.unit}`;
        return `Demasiado pequeño: se esperaba que ${z.origin} fuera ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Cadena inválida: debe comenzar con "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Cadena inválida: debe terminar en "${w.suffix}"`;
        if (w.format === "includes")
          return `Cadena inválida: debe incluir "${w.includes}"`;
        if (w.format === "regex")
          return `Cadena inválida: debe coincidir con el patrón ${w.pattern}`;
        return `Inválido ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Número inválido: debe ser múltiplo de ${z.divisor}`;
      case "unrecognized_keys":
        return `Llave${z.keys.length > 1 ? "s" : ""} desconocida${z.keys.length > 1 ? "s" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Llave inválida en ${z.origin}`;
      case "invalid_union":
        return "Entrada inválida";
      case "invalid_element":
        return `Valor inválido en ${z.origin}`;
      default:
        return "Entrada inválida";
    }
  };
};
var ee8 = E(() => {
  A3();
});
function ku1() {
  return { localeError: Gcq() };
}
var Gcq = () => {
  let A = {
    string: { unit: "کاراکتر", verb: "داشته باشد" },
    file: { unit: "بایت", verb: "داشته باشد" },
    array: { unit: "آیتم", verb: "داشته باشد" },
    set: { unit: "آیتم", verb: "داشته باشد" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "عدد";
        case "object": {
          if (Array.isArray(z)) return "آرایه";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "ورودی",
      email: "آدرس ایمیل",
      url: "URL",
      emoji: "ایموجی",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "تاریخ و زمان ایزو",
      date: "تاریخ ایزو",
      time: "زمان ایزو",
      duration: "مدت زمان ایزو",
      ipv4: "IPv4 آدرس",
      ipv6: "IPv6 آدرس",
      cidrv4: "IPv4 دامنه",
      cidrv6: "IPv6 دامنه",
      base64: "base64-encoded رشته",
      base64url: "base64url-encoded رشته",
      json_string: "JSON رشته",
      e164: "E.164 عدد",
      jwt: "JWT",
      template_literal: "ورودی",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `ورودی نامعتبر: می‌بایست ${z.expected} می‌بود، ${K(z.input)} دریافت شد`;
      case "invalid_value":
        if (z.values.length === 1)
          return `ورودی نامعتبر: می‌بایست ${g7(z.values[0])} می‌بود`;
        return `گزینه نامعتبر: می‌بایست یکی از ${XA(z.values, "|")} می‌بود`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `خیلی بزرگ: ${z.origin ?? "مقدار"} باید ${w}${z.maximum.toString()} ${_.unit ?? "عنصر"} باشد`;
        return `خیلی بزرگ: ${z.origin ?? "مقدار"} باید ${w}${z.maximum.toString()} باشد`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `خیلی کوچک: ${z.origin} باید ${w}${z.minimum.toString()} ${_.unit} باشد`;
        return `خیلی کوچک: ${z.origin} باید ${w}${z.minimum.toString()} باشد`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `رشته نامعتبر: باید با "${w.prefix}" شروع شود`;
        if (w.format === "ends_with")
          return `رشته نامعتبر: باید با "${w.suffix}" تمام شود`;
        if (w.format === "includes")
          return `رشته نامعتبر: باید شامل "${w.includes}" باشد`;
        if (w.format === "regex")
          return `رشته نامعتبر: باید با الگوی ${w.pattern} مطابقت داشته باشد`;
        return `${Y[w.format] ?? z.format} نامعتبر`;
      }
      case "not_multiple_of":
        return `عدد نامعتبر: باید مضرب ${z.divisor} باشد`;
      case "unrecognized_keys":
        return `کلید${z.keys.length > 1 ? "های" : ""} ناشناس: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `کلید ناشناس در ${z.origin}`;
      case "invalid_union":
        return "ورودی نامعتبر";
      case "invalid_element":
        return `مقدار نامعتبر در ${z.origin}`;
      default:
        return "ورودی نامعتبر";
    }
  };
};
var A6A = E(() => {
  A3();
});
function Eu1() {
  return { localeError: Zcq() };
}
var Zcq = () => {
  let A = {
    string: { unit: "merkkiä", subject: "merkkijonon" },
    file: { unit: "tavua", subject: "tiedoston" },
    array: { unit: "alkiota", subject: "listan" },
    set: { unit: "alkiota", subject: "joukon" },
    number: { unit: "", subject: "luvun" },
    bigint: { unit: "", subject: "suuren kokonaisluvun" },
    int: { unit: "", subject: "kokonaisluvun" },
    date: { unit: "", subject: "päivämäärän" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "säännöllinen lauseke",
      email: "sähköpostiosoite",
      url: "URL-osoite",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO-aikaleima",
      date: "ISO-päivämäärä",
      time: "ISO-aika",
      duration: "ISO-kesto",
      ipv4: "IPv4-osoite",
      ipv6: "IPv6-osoite",
      cidrv4: "IPv4-alue",
      cidrv6: "IPv6-alue",
      base64: "base64-koodattu merkkijono",
      base64url: "base64url-koodattu merkkijono",
      json_string: "JSON-merkkijono",
      e164: "E.164-luku",
      jwt: "JWT",
      template_literal: "templaattimerkkijono",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Virheellinen tyyppi: odotettiin ${z.expected}, oli ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Virheellinen syöte: täytyy olla ${g7(z.values[0])}`;
        return `Virheellinen valinta: täytyy olla yksi seuraavista: ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Liian suuri: ${_.subject} täytyy olla ${w}${z.maximum.toString()} ${_.unit}`.trim();
        return `Liian suuri: arvon täytyy olla ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Liian pieni: ${_.subject} täytyy olla ${w}${z.minimum.toString()} ${_.unit}`.trim();
        return `Liian pieni: arvon täytyy olla ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Virheellinen syöte: täytyy alkaa "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Virheellinen syöte: täytyy loppua "${w.suffix}"`;
        if (w.format === "includes")
          return `Virheellinen syöte: täytyy sisältää "${w.includes}"`;
        if (w.format === "regex")
          return `Virheellinen syöte: täytyy vastata säännöllistä lauseketta ${w.pattern}`;
        return `Virheellinen ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Virheellinen luku: täytyy olla luvun ${z.divisor} monikerta`;
      case "unrecognized_keys":
        return `${z.keys.length > 1 ? "Tuntemattomat avaimet" : "Tuntematon avain"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return "Virheellinen avain tietueessa";
      case "invalid_union":
        return "Virheellinen unioni";
      case "invalid_element":
        return "Virheellinen arvo joukossa";
      default:
        return "Virheellinen syöte";
    }
  };
};
var q6A = E(() => {
  A3();
});
function Lu1() {
  return { localeError: fcq() };
}
var fcq = () => {
  let A = {
    string: { unit: "caractères", verb: "avoir" },
    file: { unit: "octets", verb: "avoir" },
    array: { unit: "éléments", verb: "avoir" },
    set: { unit: "éléments", verb: "avoir" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "nombre";
        case "object": {
          if (Array.isArray(z)) return "tableau";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "entrée",
      email: "adresse e-mail",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "date et heure ISO",
      date: "date ISO",
      time: "heure ISO",
      duration: "durée ISO",
      ipv4: "adresse IPv4",
      ipv6: "adresse IPv6",
      cidrv4: "plage IPv4",
      cidrv6: "plage IPv6",
      base64: "chaîne encodée en base64",
      base64url: "chaîne encodée en base64url",
      json_string: "chaîne JSON",
      e164: "numéro E.164",
      jwt: "JWT",
      template_literal: "entrée",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Entrée invalide : ${z.expected} attendu, ${K(z.input)} reçu`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Entrée invalide : ${g7(z.values[0])} attendu`;
        return `Option invalide : une valeur parmi ${XA(z.values, "|")} attendue`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Trop grand : ${z.origin ?? "valeur"} doit ${_.verb} ${w}${z.maximum.toString()} ${_.unit ?? "élément(s)"}`;
        return `Trop grand : ${z.origin ?? "valeur"} doit être ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Trop petit : ${z.origin} doit ${_.verb} ${w}${z.minimum.toString()} ${_.unit}`;
        return `Trop petit : ${z.origin} doit être ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Chaîne invalide : doit commencer par "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Chaîne invalide : doit se terminer par "${w.suffix}"`;
        if (w.format === "includes")
          return `Chaîne invalide : doit inclure "${w.includes}"`;
        if (w.format === "regex")
          return `Chaîne invalide : doit correspondre au modèle ${w.pattern}`;
        return `${Y[w.format] ?? z.format} invalide`;
      }
      case "not_multiple_of":
        return `Nombre invalide : doit être un multiple de ${z.divisor}`;
      case "unrecognized_keys":
        return `Clé${z.keys.length > 1 ? "s" : ""} non reconnue${z.keys.length > 1 ? "s" : ""} : ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Clé invalide dans ${z.origin}`;
      case "invalid_union":
        return "Entrée invalide";
      case "invalid_element":
        return `Valeur invalide dans ${z.origin}`;
      default:
        return "Entrée invalide";
    }
  };
};
var K6A = E(() => {
  A3();
});
function yu1() {
  return { localeError: Tcq() };
}
var Tcq = () => {
  let A = {
    string: { unit: "caractères", verb: "avoir" },
    file: { unit: "octets", verb: "avoir" },
    array: { unit: "éléments", verb: "avoir" },
    set: { unit: "éléments", verb: "avoir" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "entrée",
      email: "adresse courriel",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "date-heure ISO",
      date: "date ISO",
      time: "heure ISO",
      duration: "durée ISO",
      ipv4: "adresse IPv4",
      ipv6: "adresse IPv6",
      cidrv4: "plage IPv4",
      cidrv6: "plage IPv6",
      base64: "chaîne encodée en base64",
      base64url: "chaîne encodée en base64url",
      json_string: "chaîne JSON",
      e164: "numéro E.164",
      jwt: "JWT",
      template_literal: "entrée",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Entrée invalide : attendu ${z.expected}, reçu ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Entrée invalide : attendu ${g7(z.values[0])}`;
        return `Option invalide : attendu l'une des valeurs suivantes ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "≤" : "<",
          _ = q(z.origin);
        if (_)
          return `Trop grand : attendu que ${z.origin ?? "la valeur"} ait ${w}${z.maximum.toString()} ${_.unit}`;
        return `Trop grand : attendu que ${z.origin ?? "la valeur"} soit ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? "≥" : ">",
          _ = q(z.origin);
        if (_)
          return `Trop petit : attendu que ${z.origin} ait ${w}${z.minimum.toString()} ${_.unit}`;
        return `Trop petit : attendu que ${z.origin} soit ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Chaîne invalide : doit commencer par "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Chaîne invalide : doit se terminer par "${w.suffix}"`;
        if (w.format === "includes")
          return `Chaîne invalide : doit inclure "${w.includes}"`;
        if (w.format === "regex")
          return `Chaîne invalide : doit correspondre au motif ${w.pattern}`;
        return `${Y[w.format] ?? z.format} invalide`;
      }
      case "not_multiple_of":
        return `Nombre invalide : doit être un multiple de ${z.divisor}`;
      case "unrecognized_keys":
        return `Clé${z.keys.length > 1 ? "s" : ""} non reconnue${z.keys.length > 1 ? "s" : ""} : ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Clé invalide dans ${z.origin}`;
      case "invalid_union":
        return "Entrée invalide";
      case "invalid_element":
        return `Valeur invalide dans ${z.origin}`;
      default:
        return "Entrée invalide";
    }
  };
};
var Y6A = E(() => {
  A3();
});
function Ru1() {
  return { localeError: Ncq() };
}
var Ncq = () => {
  let A = {
    string: { unit: "אותיות", verb: "לכלול" },
    file: { unit: "בייטים", verb: "לכלול" },
    array: { unit: "פריטים", verb: "לכלול" },
    set: { unit: "פריטים", verb: "לכלול" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "קלט",
      email: "כתובת אימייל",
      url: "כתובת רשת",
      emoji: "אימוג'י",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "תאריך וזמן ISO",
      date: "תאריך ISO",
      time: "זמן ISO",
      duration: "משך זמן ISO",
      ipv4: "כתובת IPv4",
      ipv6: "כתובת IPv6",
      cidrv4: "טווח IPv4",
      cidrv6: "טווח IPv6",
      base64: "מחרוזת בבסיס 64",
      base64url: "מחרוזת בבסיס 64 לכתובות רשת",
      json_string: "מחרוזת JSON",
      e164: "מספר E.164",
      jwt: "JWT",
      template_literal: "קלט",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `קלט לא תקין: צריך ${z.expected}, התקבל ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `קלט לא תקין: צריך ${g7(z.values[0])}`;
        return `קלט לא תקין: צריך אחת מהאפשרויות  ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `גדול מדי: ${z.origin ?? "value"} צריך להיות ${w}${z.maximum.toString()} ${_.unit ?? "elements"}`;
        return `גדול מדי: ${z.origin ?? "value"} צריך להיות ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `קטן מדי: ${z.origin} צריך להיות ${w}${z.minimum.toString()} ${_.unit}`;
        return `קטן מדי: ${z.origin} צריך להיות ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `מחרוזת לא תקינה: חייבת להתחיל ב"${w.prefix}"`;
        if (w.format === "ends_with")
          return `מחרוזת לא תקינה: חייבת להסתיים ב "${w.suffix}"`;
        if (w.format === "includes")
          return `מחרוזת לא תקינה: חייבת לכלול "${w.includes}"`;
        if (w.format === "regex")
          return `מחרוזת לא תקינה: חייבת להתאים לתבנית ${w.pattern}`;
        return `${Y[w.format] ?? z.format} לא תקין`;
      }
      case "not_multiple_of":
        return `מספר לא תקין: חייב להיות מכפלה של ${z.divisor}`;
      case "unrecognized_keys":
        return `מפתח${z.keys.length > 1 ? "ות" : ""} לא מזוה${z.keys.length > 1 ? "ים" : "ה"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `מפתח לא תקין ב${z.origin}`;
      case "invalid_union":
        return "קלט לא תקין";
      case "invalid_element":
        return `ערך לא תקין ב${z.origin}`;
      default:
        return "קלט לא תקין";
    }
  };
};
var z6A = E(() => {
  A3();
});
function Cu1() {
  return { localeError: Vcq() };
}
var Vcq = () => {
  let A = {
    string: { unit: "karakter", verb: "legyen" },
    file: { unit: "byte", verb: "legyen" },
    array: { unit: "elem", verb: "legyen" },
    set: { unit: "elem", verb: "legyen" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "szám";
        case "object": {
          if (Array.isArray(z)) return "tömb";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "bemenet",
      email: "email cím",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO időbélyeg",
      date: "ISO dátum",
      time: "ISO idő",
      duration: "ISO időintervallum",
      ipv4: "IPv4 cím",
      ipv6: "IPv6 cím",
      cidrv4: "IPv4 tartomány",
      cidrv6: "IPv6 tartomány",
      base64: "base64-kódolt string",
      base64url: "base64url-kódolt string",
      json_string: "JSON string",
      e164: "E.164 szám",
      jwt: "JWT",
      template_literal: "bemenet",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Érvénytelen bemenet: a várt érték ${z.expected}, a kapott érték ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Érvénytelen bemenet: a várt érték ${g7(z.values[0])}`;
        return `Érvénytelen opció: valamelyik érték várt ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Túl nagy: ${z.origin ?? "érték"} mérete túl nagy ${w}${z.maximum.toString()} ${_.unit ?? "elem"}`;
        return `Túl nagy: a bemeneti érték ${z.origin ?? "érték"} túl nagy: ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Túl kicsi: a bemeneti érték ${z.origin} mérete túl kicsi ${w}${z.minimum.toString()} ${_.unit}`;
        return `Túl kicsi: a bemeneti érték ${z.origin} túl kicsi ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Érvénytelen string: "${w.prefix}" értékkel kell kezdődnie`;
        if (w.format === "ends_with")
          return `Érvénytelen string: "${w.suffix}" értékkel kell végződnie`;
        if (w.format === "includes")
          return `Érvénytelen string: "${w.includes}" értéket kell tartalmaznia`;
        if (w.format === "regex")
          return `Érvénytelen string: ${w.pattern} mintának kell megfelelnie`;
        return `Érvénytelen ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Érvénytelen szám: ${z.divisor} többszörösének kell lennie`;
      case "unrecognized_keys":
        return `Ismeretlen kulcs${z.keys.length > 1 ? "s" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Érvénytelen kulcs ${z.origin}`;
      case "invalid_union":
        return "Érvénytelen bemenet";
      case "invalid_element":
        return `Érvénytelen érték: ${z.origin}`;
      default:
        return "Érvénytelen bemenet";
    }
  };
};
var w6A = E(() => {
  A3();
});
function Su1() {
  return { localeError: vcq() };
}
var vcq = () => {
  let A = {
    string: { unit: "karakter", verb: "memiliki" },
    file: { unit: "byte", verb: "memiliki" },
    array: { unit: "item", verb: "memiliki" },
    set: { unit: "item", verb: "memiliki" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "input",
      email: "alamat email",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "tanggal dan waktu format ISO",
      date: "tanggal format ISO",
      time: "jam format ISO",
      duration: "durasi format ISO",
      ipv4: "alamat IPv4",
      ipv6: "alamat IPv6",
      cidrv4: "rentang alamat IPv4",
      cidrv6: "rentang alamat IPv6",
      base64: "string dengan enkode base64",
      base64url: "string dengan enkode base64url",
      json_string: "string JSON",
      e164: "angka E.164",
      jwt: "JWT",
      template_literal: "input",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Input tidak valid: diharapkan ${z.expected}, diterima ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Input tidak valid: diharapkan ${g7(z.values[0])}`;
        return `Pilihan tidak valid: diharapkan salah satu dari ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Terlalu besar: diharapkan ${z.origin ?? "value"} memiliki ${w}${z.maximum.toString()} ${_.unit ?? "elemen"}`;
        return `Terlalu besar: diharapkan ${z.origin ?? "value"} menjadi ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Terlalu kecil: diharapkan ${z.origin} memiliki ${w}${z.minimum.toString()} ${_.unit}`;
        return `Terlalu kecil: diharapkan ${z.origin} menjadi ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `String tidak valid: harus dimulai dengan "${w.prefix}"`;
        if (w.format === "ends_with")
          return `String tidak valid: harus berakhir dengan "${w.suffix}"`;
        if (w.format === "includes")
          return `String tidak valid: harus menyertakan "${w.includes}"`;
        if (w.format === "regex")
          return `String tidak valid: harus sesuai pola ${w.pattern}`;
        return `${Y[w.format] ?? z.format} tidak valid`;
      }
      case "not_multiple_of":
        return `Angka tidak valid: harus kelipatan dari ${z.divisor}`;
      case "unrecognized_keys":
        return `Kunci tidak dikenali ${z.keys.length > 1 ? "s" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Kunci tidak valid di ${z.origin}`;
      case "invalid_union":
        return "Input tidak valid";
      case "invalid_element":
        return `Nilai tidak valid di ${z.origin}`;
      default:
        return "Input tidak valid";
    }
  };
};
var _6A = E(() => {
  A3();
});
function hu1() {
  return { localeError: kcq() };
}
var kcq = () => {
  let A = {
    string: { unit: "caratteri", verb: "avere" },
    file: { unit: "byte", verb: "avere" },
    array: { unit: "elementi", verb: "avere" },
    set: { unit: "elementi", verb: "avere" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "numero";
        case "object": {
          if (Array.isArray(z)) return "vettore";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "input",
      email: "indirizzo email",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "data e ora ISO",
      date: "data ISO",
      time: "ora ISO",
      duration: "durata ISO",
      ipv4: "indirizzo IPv4",
      ipv6: "indirizzo IPv6",
      cidrv4: "intervallo IPv4",
      cidrv6: "intervallo IPv6",
      base64: "stringa codificata in base64",
      base64url: "URL codificata in base64",
      json_string: "stringa JSON",
      e164: "numero E.164",
      jwt: "JWT",
      template_literal: "input",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Input non valido: atteso ${z.expected}, ricevuto ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Input non valido: atteso ${g7(z.values[0])}`;
        return `Opzione non valida: atteso uno tra ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Troppo grande: ${z.origin ?? "valore"} deve avere ${w}${z.maximum.toString()} ${_.unit ?? "elementi"}`;
        return `Troppo grande: ${z.origin ?? "valore"} deve essere ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Troppo piccolo: ${z.origin} deve avere ${w}${z.minimum.toString()} ${_.unit}`;
        return `Troppo piccolo: ${z.origin} deve essere ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Stringa non valida: deve iniziare con "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Stringa non valida: deve terminare con "${w.suffix}"`;
        if (w.format === "includes")
          return `Stringa non valida: deve includere "${w.includes}"`;
        if (w.format === "regex")
          return `Stringa non valida: deve corrispondere al pattern ${w.pattern}`;
        return `Invalid ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Numero non valido: deve essere un multiplo di ${z.divisor}`;
      case "unrecognized_keys":
        return `Chiav${z.keys.length > 1 ? "i" : "e"} non riconosciut${z.keys.length > 1 ? "e" : "a"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Chiave non valida in ${z.origin}`;
      case "invalid_union":
        return "Input non valido";
      case "invalid_element":
        return `Valore non valido in ${z.origin}`;
      default:
        return "Input non valido";
    }
  };
};
var $6A = E(() => {
  A3();
});
function Iu1() {
  return { localeError: Ecq() };
}
var Ecq = () => {
  let A = {
    string: { unit: "文字", verb: "である" },
    file: { unit: "バイト", verb: "である" },
    array: { unit: "要素", verb: "である" },
    set: { unit: "要素", verb: "である" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "数値";
        case "object": {
          if (Array.isArray(z)) return "配列";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "入力値",
      email: "メールアドレス",
      url: "URL",
      emoji: "絵文字",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO日時",
      date: "ISO日付",
      time: "ISO時刻",
      duration: "ISO期間",
      ipv4: "IPv4アドレス",
      ipv6: "IPv6アドレス",
      cidrv4: "IPv4範囲",
      cidrv6: "IPv6範囲",
      base64: "base64エンコード文字列",
      base64url: "base64urlエンコード文字列",
      json_string: "JSON文字列",
      e164: "E.164番号",
      jwt: "JWT",
      template_literal: "入力値",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `無効な入力: ${z.expected}が期待されましたが、${K(z.input)}が入力されました`;
      case "invalid_value":
        if (z.values.length === 1)
          return `無効な入力: ${g7(z.values[0])}が期待されました`;
        return `無効な選択: ${XA(z.values, "、")}のいずれかである必要があります`;
      case "too_big": {
        let w = z.inclusive ? "以下である" : "より小さい",
          _ = q(z.origin);
        if (_)
          return `大きすぎる値: ${z.origin ?? "値"}は${z.maximum.toString()}${_.unit ?? "要素"}${w}必要があります`;
        return `大きすぎる値: ${z.origin ?? "値"}は${z.maximum.toString()}${w}必要があります`;
      }
      case "too_small": {
        let w = z.inclusive ? "以上である" : "より大きい",
          _ = q(z.origin);
        if (_)
          return `小さすぎる値: ${z.origin}は${z.minimum.toString()}${_.unit}${w}必要があります`;
        return `小さすぎる値: ${z.origin}は${z.minimum.toString()}${w}必要があります`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `無効な文字列: "${w.prefix}"で始まる必要があります`;
        if (w.format === "ends_with")
          return `無効な文字列: "${w.suffix}"で終わる必要があります`;
        if (w.format === "includes")
          return `無効な文字列: "${w.includes}"を含む必要があります`;
        if (w.format === "regex")
          return `無効な文字列: パターン${w.pattern}に一致する必要があります`;
        return `無効な${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `無効な数値: ${z.divisor}の倍数である必要があります`;
      case "unrecognized_keys":
        return `認識されていないキー${z.keys.length > 1 ? "群" : ""}: ${XA(z.keys, "、")}`;
      case "invalid_key":
        return `${z.origin}内の無効なキー`;
      case "invalid_union":
        return "無効な入力";
      case "invalid_element":
        return `${z.origin}内の無効な値`;
      default:
        return "無効な入力";
    }
  };
};
var O6A = E(() => {
  A3();
});
function xu1() {
  return { localeError: Lcq() };
}
var Lcq = () => {
  let A = {
    string: { unit: "តួអក្សរ", verb: "គួរមាន" },
    file: { unit: "បៃ", verb: "គួរមាន" },
    array: { unit: "ធាតុ", verb: "គួរមាន" },
    set: { unit: "ធាតុ", verb: "គួរមាន" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "មិនមែនជាលេខ (NaN)" : "លេខ";
        case "object": {
          if (Array.isArray(z)) return "អារេ (Array)";
          if (z === null) return "គ្មានតម្លៃ (null)";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "ទិន្នន័យបញ្ចូល",
      email: "អាសយដ្ឋានអ៊ីមែល",
      url: "URL",
      emoji: "សញ្ញាអារម្មណ៍",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "កាលបរិច្ឆេទ និងម៉ោង ISO",
      date: "កាលបរិច្ឆេទ ISO",
      time: "ម៉ោង ISO",
      duration: "រយៈពេល ISO",
      ipv4: "អាសយដ្ឋាន IPv4",
      ipv6: "អាសយដ្ឋាន IPv6",
      cidrv4: "ដែនអាសយដ្ឋាន IPv4",
      cidrv6: "ដែនអាសយដ្ឋាន IPv6",
      base64: "ខ្សែអក្សរអ៊ិកូដ base64",
      base64url: "ខ្សែអក្សរអ៊ិកូដ base64url",
      json_string: "ខ្សែអក្សរ JSON",
      e164: "លេខ E.164",
      jwt: "JWT",
      template_literal: "ទិន្នន័យបញ្ចូល",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `ទិន្នន័យបញ្ចូលមិនត្រឹមត្រូវ៖ ត្រូវការ ${z.expected} ប៉ុន្តែទទួលបាន ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `ទិន្នន័យបញ្ចូលមិនត្រឹមត្រូវ៖ ត្រូវការ ${g7(z.values[0])}`;
        return `ជម្រើសមិនត្រឹមត្រូវ៖ ត្រូវជាមួយក្នុងចំណោម ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `ធំពេក៖ ត្រូវការ ${z.origin ?? "តម្លៃ"} ${w} ${z.maximum.toString()} ${_.unit ?? "ធាតុ"}`;
        return `ធំពេក៖ ត្រូវការ ${z.origin ?? "តម្លៃ"} ${w} ${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `តូចពេក៖ ត្រូវការ ${z.origin} ${w} ${z.minimum.toString()} ${_.unit}`;
        return `តូចពេក៖ ត្រូវការ ${z.origin} ${w} ${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `ខ្សែអក្សរមិនត្រឹមត្រូវ៖ ត្រូវចាប់ផ្តើមដោយ "${w.prefix}"`;
        if (w.format === "ends_with")
          return `ខ្សែអក្សរមិនត្រឹមត្រូវ៖ ត្រូវបញ្ចប់ដោយ "${w.suffix}"`;
        if (w.format === "includes")
          return `ខ្សែអក្សរមិនត្រឹមត្រូវ៖ ត្រូវមាន "${w.includes}"`;
        if (w.format === "regex")
          return `ខ្សែអក្សរមិនត្រឹមត្រូវ៖ ត្រូវតែផ្គូផ្គងនឹងទម្រង់ដែលបានកំណត់ ${w.pattern}`;
        return `មិនត្រឹមត្រូវ៖ ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `លេខមិនត្រឹមត្រូវ៖ ត្រូវតែជាពហុគុណនៃ ${z.divisor}`;
      case "unrecognized_keys":
        return `រកឃើញសោមិនស្គាល់៖ ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `សោមិនត្រឹមត្រូវនៅក្នុង ${z.origin}`;
      case "invalid_union":
        return "ទិន្នន័យមិនត្រឹមត្រូវ";
      case "invalid_element":
        return `ទិន្នន័យមិនត្រឹមត្រូវនៅក្នុង ${z.origin}`;
      default:
        return "ទិន្នន័យមិនត្រឹមត្រូវ";
    }
  };
};
var H6A = E(() => {
  A3();
});
function bu1() {
  return { localeError: ycq() };
}
var ycq = () => {
  let A = {
    string: { unit: "문자", verb: "to have" },
    file: { unit: "바이트", verb: "to have" },
    array: { unit: "개", verb: "to have" },
    set: { unit: "개", verb: "to have" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "입력",
      email: "이메일 주소",
      url: "URL",
      emoji: "이모지",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO 날짜시간",
      date: "ISO 날짜",
      time: "ISO 시간",
      duration: "ISO 기간",
      ipv4: "IPv4 주소",
      ipv6: "IPv6 주소",
      cidrv4: "IPv4 범위",
      cidrv6: "IPv6 범위",
      base64: "base64 인코딩 문자열",
      base64url: "base64url 인코딩 문자열",
      json_string: "JSON 문자열",
      e164: "E.164 번호",
      jwt: "JWT",
      template_literal: "입력",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `잘못된 입력: 예상 타입은 ${z.expected}, 받은 타입은 ${K(z.input)}입니다`;
      case "invalid_value":
        if (z.values.length === 1)
          return `잘못된 입력: 값은 ${g7(z.values[0])} 이어야 합니다`;
        return `잘못된 옵션: ${XA(z.values, "또는 ")} 중 하나여야 합니다`;
      case "too_big": {
        let w = z.inclusive ? "이하" : "미만",
          _ = w === "미만" ? "이어야 합니다" : "여야 합니다",
          $ = q(z.origin),
          O = $?.unit ?? "요소";
        if ($)
          return `${z.origin ?? "값"}이 너무 큽니다: ${z.maximum.toString()}${O} ${w}${_}`;
        return `${z.origin ?? "값"}이 너무 큽니다: ${z.maximum.toString()} ${w}${_}`;
      }
      case "too_small": {
        let w = z.inclusive ? "이상" : "초과",
          _ = w === "이상" ? "이어야 합니다" : "여야 합니다",
          $ = q(z.origin),
          O = $?.unit ?? "요소";
        if ($)
          return `${z.origin ?? "값"}이 너무 작습니다: ${z.minimum.toString()}${O} ${w}${_}`;
        return `${z.origin ?? "값"}이 너무 작습니다: ${z.minimum.toString()} ${w}${_}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `잘못된 문자열: "${w.prefix}"(으)로 시작해야 합니다`;
        if (w.format === "ends_with")
          return `잘못된 문자열: "${w.suffix}"(으)로 끝나야 합니다`;
        if (w.format === "includes")
          return `잘못된 문자열: "${w.includes}"을(를) 포함해야 합니다`;
        if (w.format === "regex")
          return `잘못된 문자열: 정규식 ${w.pattern} 패턴과 일치해야 합니다`;
        return `잘못된 ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `잘못된 숫자: ${z.divisor}의 배수여야 합니다`;
      case "unrecognized_keys":
        return `인식할 수 없는 키: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `잘못된 키: ${z.origin}`;
      case "invalid_union":
        return "잘못된 입력";
      case "invalid_element":
        return `잘못된 값: ${z.origin}`;
      default:
        return "잘못된 입력";
    }
  };
};
var j6A = E(() => {
  A3();
});
function uu1() {
  return { localeError: Rcq() };
}
var Rcq = () => {
  let A = {
    string: { unit: "знаци", verb: "да имаат" },
    file: { unit: "бајти", verb: "да имаат" },
    array: { unit: "ставки", verb: "да имаат" },
    set: { unit: "ставки", verb: "да имаат" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "број";
        case "object": {
          if (Array.isArray(z)) return "низа";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "внес",
      email: "адреса на е-пошта",
      url: "URL",
      emoji: "емоџи",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO датум и време",
      date: "ISO датум",
      time: "ISO време",
      duration: "ISO времетраење",
      ipv4: "IPv4 адреса",
      ipv6: "IPv6 адреса",
      cidrv4: "IPv4 опсег",
      cidrv6: "IPv6 опсег",
      base64: "base64-енкодирана низа",
      base64url: "base64url-енкодирана низа",
      json_string: "JSON низа",
      e164: "E.164 број",
      jwt: "JWT",
      template_literal: "внес",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Грешен внес: се очекува ${z.expected}, примено ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Invalid input: expected ${g7(z.values[0])}`;
        return `Грешана опција: се очекува една ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Премногу голем: се очекува ${z.origin ?? "вредноста"} да има ${w}${z.maximum.toString()} ${_.unit ?? "елементи"}`;
        return `Премногу голем: се очекува ${z.origin ?? "вредноста"} да биде ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Премногу мал: се очекува ${z.origin} да има ${w}${z.minimum.toString()} ${_.unit}`;
        return `Премногу мал: се очекува ${z.origin} да биде ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Неважечка низа: мора да започнува со "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Неважечка низа: мора да завршува со "${w.suffix}"`;
        if (w.format === "includes")
          return `Неважечка низа: мора да вклучува "${w.includes}"`;
        if (w.format === "regex")
          return `Неважечка низа: мора да одгоара на патернот ${w.pattern}`;
        return `Invalid ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Грешен број: мора да биде делив со ${z.divisor}`;
      case "unrecognized_keys":
        return `${z.keys.length > 1 ? "Непрепознаени клучеви" : "Непрепознаен клуч"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Грешен клуч во ${z.origin}`;
      case "invalid_union":
        return "Грешен внес";
      case "invalid_element":
        return `Грешна вредност во ${z.origin}`;
      default:
        return "Грешен внес";
    }
  };
};
var J6A = E(() => {
  A3();
});
function mu1() {
  return { localeError: Ccq() };
}
var Ccq = () => {
  let A = {
    string: { unit: "aksara", verb: "mempunyai" },
    file: { unit: "bait", verb: "mempunyai" },
    array: { unit: "elemen", verb: "mempunyai" },
    set: { unit: "elemen", verb: "mempunyai" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "nombor";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "input",
      email: "alamat e-mel",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "tarikh masa ISO",
      date: "tarikh ISO",
      time: "masa ISO",
      duration: "tempoh ISO",
      ipv4: "alamat IPv4",
      ipv6: "alamat IPv6",
      cidrv4: "julat IPv4",
      cidrv6: "julat IPv6",
      base64: "string dikodkan base64",
      base64url: "string dikodkan base64url",
      json_string: "string JSON",
      e164: "nombor E.164",
      jwt: "JWT",
      template_literal: "input",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Input tidak sah: dijangka ${z.expected}, diterima ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Input tidak sah: dijangka ${g7(z.values[0])}`;
        return `Pilihan tidak sah: dijangka salah satu daripada ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Terlalu besar: dijangka ${z.origin ?? "nilai"} ${_.verb} ${w}${z.maximum.toString()} ${_.unit ?? "elemen"}`;
        return `Terlalu besar: dijangka ${z.origin ?? "nilai"} adalah ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Terlalu kecil: dijangka ${z.origin} ${_.verb} ${w}${z.minimum.toString()} ${_.unit}`;
        return `Terlalu kecil: dijangka ${z.origin} adalah ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `String tidak sah: mesti bermula dengan "${w.prefix}"`;
        if (w.format === "ends_with")
          return `String tidak sah: mesti berakhir dengan "${w.suffix}"`;
        if (w.format === "includes")
          return `String tidak sah: mesti mengandungi "${w.includes}"`;
        if (w.format === "regex")
          return `String tidak sah: mesti sepadan dengan corak ${w.pattern}`;
        return `${Y[w.format] ?? z.format} tidak sah`;
      }
      case "not_multiple_of":
        return `Nombor tidak sah: perlu gandaan ${z.divisor}`;
      case "unrecognized_keys":
        return `Kunci tidak dikenali: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Kunci tidak sah dalam ${z.origin}`;
      case "invalid_union":
        return "Input tidak sah";
      case "invalid_element":
        return `Nilai tidak sah dalam ${z.origin}`;
      default:
        return "Input tidak sah";
    }
  };
};
var D6A = E(() => {
  A3();
});
function Bu1() {
  return { localeError: Scq() };
}
var Scq = () => {
  let A = {
    string: { unit: "tekens" },
    file: { unit: "bytes" },
    array: { unit: "elementen" },
    set: { unit: "elementen" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "getal";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "invoer",
      email: "emailadres",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO datum en tijd",
      date: "ISO datum",
      time: "ISO tijd",
      duration: "ISO duur",
      ipv4: "IPv4-adres",
      ipv6: "IPv6-adres",
      cidrv4: "IPv4-bereik",
      cidrv6: "IPv6-bereik",
      base64: "base64-gecodeerde tekst",
      base64url: "base64 URL-gecodeerde tekst",
      json_string: "JSON string",
      e164: "E.164-nummer",
      jwt: "JWT",
      template_literal: "invoer",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Ongeldige invoer: verwacht ${z.expected}, ontving ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Ongeldige invoer: verwacht ${g7(z.values[0])}`;
        return `Ongeldige optie: verwacht één van ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Te lang: verwacht dat ${z.origin ?? "waarde"} ${w}${z.maximum.toString()} ${_.unit ?? "elementen"} bevat`;
        return `Te lang: verwacht dat ${z.origin ?? "waarde"} ${w}${z.maximum.toString()} is`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Te kort: verwacht dat ${z.origin} ${w}${z.minimum.toString()} ${_.unit} bevat`;
        return `Te kort: verwacht dat ${z.origin} ${w}${z.minimum.toString()} is`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Ongeldige tekst: moet met "${w.prefix}" beginnen`;
        if (w.format === "ends_with")
          return `Ongeldige tekst: moet op "${w.suffix}" eindigen`;
        if (w.format === "includes")
          return `Ongeldige tekst: moet "${w.includes}" bevatten`;
        if (w.format === "regex")
          return `Ongeldige tekst: moet overeenkomen met patroon ${w.pattern}`;
        return `Ongeldig: ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Ongeldig getal: moet een veelvoud van ${z.divisor} zijn`;
      case "unrecognized_keys":
        return `Onbekende key${z.keys.length > 1 ? "s" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Ongeldige key in ${z.origin}`;
      case "invalid_union":
        return "Ongeldige invoer";
      case "invalid_element":
        return `Ongeldige waarde in ${z.origin}`;
      default:
        return "Ongeldige invoer";
    }
  };
};
var X6A = E(() => {
  A3();
});
function gu1() {
  return { localeError: hcq() };
}
var hcq = () => {
  let A = {
    string: { unit: "tegn", verb: "å ha" },
    file: { unit: "bytes", verb: "å ha" },
    array: { unit: "elementer", verb: "å inneholde" },
    set: { unit: "elementer", verb: "å inneholde" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "tall";
        case "object": {
          if (Array.isArray(z)) return "liste";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "input",
      email: "e-postadresse",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO dato- og klokkeslett",
      date: "ISO-dato",
      time: "ISO-klokkeslett",
      duration: "ISO-varighet",
      ipv4: "IPv4-område",
      ipv6: "IPv6-område",
      cidrv4: "IPv4-spekter",
      cidrv6: "IPv6-spekter",
      base64: "base64-enkodet streng",
      base64url: "base64url-enkodet streng",
      json_string: "JSON-streng",
      e164: "E.164-nummer",
      jwt: "JWT",
      template_literal: "input",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Ugyldig input: forventet ${z.expected}, fikk ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Ugyldig verdi: forventet ${g7(z.values[0])}`;
        return `Ugyldig valg: forventet en av ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `For stor(t): forventet ${z.origin ?? "value"} til å ha ${w}${z.maximum.toString()} ${_.unit ?? "elementer"}`;
        return `For stor(t): forventet ${z.origin ?? "value"} til å ha ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `For lite(n): forventet ${z.origin} til å ha ${w}${z.minimum.toString()} ${_.unit}`;
        return `For lite(n): forventet ${z.origin} til å ha ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Ugyldig streng: må starte med "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Ugyldig streng: må ende med "${w.suffix}"`;
        if (w.format === "includes")
          return `Ugyldig streng: må inneholde "${w.includes}"`;
        if (w.format === "regex")
          return `Ugyldig streng: må matche mønsteret ${w.pattern}`;
        return `Ugyldig ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Ugyldig tall: må være et multiplum av ${z.divisor}`;
      case "unrecognized_keys":
        return `${z.keys.length > 1 ? "Ukjente nøkler" : "Ukjent nøkkel"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Ugyldig nøkkel i ${z.origin}`;
      case "invalid_union":
        return "Ugyldig input";
      case "invalid_element":
        return `Ugyldig verdi i ${z.origin}`;
      default:
        return "Ugyldig input";
    }
  };
};
var M6A = E(() => {
  A3();
});
function Fu1() {
  return { localeError: Icq() };
}
var Icq = () => {
  let A = {
    string: { unit: "harf", verb: "olmalıdır" },
    file: { unit: "bayt", verb: "olmalıdır" },
    array: { unit: "unsur", verb: "olmalıdır" },
    set: { unit: "unsur", verb: "olmalıdır" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "numara";
        case "object": {
          if (Array.isArray(z)) return "saf";
          if (z === null) return "gayb";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "giren",
      email: "epostagâh",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO hengâmı",
      date: "ISO tarihi",
      time: "ISO zamanı",
      duration: "ISO müddeti",
      ipv4: "IPv4 nişânı",
      ipv6: "IPv6 nişânı",
      cidrv4: "IPv4 menzili",
      cidrv6: "IPv6 menzili",
      base64: "base64-şifreli metin",
      base64url: "base64url-şifreli metin",
      json_string: "JSON metin",
      e164: "E.164 sayısı",
      jwt: "JWT",
      template_literal: "giren",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Fâsit giren: umulan ${z.expected}, alınan ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Fâsit giren: umulan ${g7(z.values[0])}`;
        return `Fâsit tercih: mûteberler ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Fazla büyük: ${z.origin ?? "value"}, ${w}${z.maximum.toString()} ${_.unit ?? "elements"} sahip olmalıydı.`;
        return `Fazla büyük: ${z.origin ?? "value"}, ${w}${z.maximum.toString()} olmalıydı.`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Fazla küçük: ${z.origin}, ${w}${z.minimum.toString()} ${_.unit} sahip olmalıydı.`;
        return `Fazla küçük: ${z.origin}, ${w}${z.minimum.toString()} olmalıydı.`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Fâsit metin: "${w.prefix}" ile başlamalı.`;
        if (w.format === "ends_with")
          return `Fâsit metin: "${w.suffix}" ile bitmeli.`;
        if (w.format === "includes")
          return `Fâsit metin: "${w.includes}" ihtivâ etmeli.`;
        if (w.format === "regex")
          return `Fâsit metin: ${w.pattern} nakşına uymalı.`;
        return `Fâsit ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Fâsit sayı: ${z.divisor} katı olmalıydı.`;
      case "unrecognized_keys":
        return `Tanınmayan anahtar ${z.keys.length > 1 ? "s" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `${z.origin} için tanınmayan anahtar var.`;
      case "invalid_union":
        return "Giren tanınamadı.";
      case "invalid_element":
        return `${z.origin} için tanınmayan kıymet var.`;
      default:
        return "Kıymet tanınamadı.";
    }
  };
};
var P6A = E(() => {
  A3();
});
function pu1() {
  return { localeError: xcq() };
}
var xcq = () => {
  let A = {
    string: { unit: "توکي", verb: "ولري" },
    file: { unit: "بایټس", verb: "ولري" },
    array: { unit: "توکي", verb: "ولري" },
    set: { unit: "توکي", verb: "ولري" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "عدد";
        case "object": {
          if (Array.isArray(z)) return "ارې";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "ورودي",
      email: "بریښنالیک",
      url: "یو آر ال",
      emoji: "ایموجي",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "نیټه او وخت",
      date: "نېټه",
      time: "وخت",
      duration: "موده",
      ipv4: "د IPv4 پته",
      ipv6: "د IPv6 پته",
      cidrv4: "د IPv4 ساحه",
      cidrv6: "د IPv6 ساحه",
      base64: "base64-encoded متن",
      base64url: "base64url-encoded متن",
      json_string: "JSON متن",
      e164: "د E.164 شمېره",
      jwt: "JWT",
      template_literal: "ورودي",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `ناسم ورودي: باید ${z.expected} وای, مګر ${K(z.input)} ترلاسه شو`;
      case "invalid_value":
        if (z.values.length === 1)
          return `ناسم ورودي: باید ${g7(z.values[0])} وای`;
        return `ناسم انتخاب: باید یو له ${XA(z.values, "|")} څخه وای`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `ډیر لوی: ${z.origin ?? "ارزښت"} باید ${w}${z.maximum.toString()} ${_.unit ?? "عنصرونه"} ولري`;
        return `ډیر لوی: ${z.origin ?? "ارزښت"} باید ${w}${z.maximum.toString()} وي`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `ډیر کوچنی: ${z.origin} باید ${w}${z.minimum.toString()} ${_.unit} ولري`;
        return `ډیر کوچنی: ${z.origin} باید ${w}${z.minimum.toString()} وي`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `ناسم متن: باید د "${w.prefix}" سره پیل شي`;
        if (w.format === "ends_with")
          return `ناسم متن: باید د "${w.suffix}" سره پای ته ورسيږي`;
        if (w.format === "includes")
          return `ناسم متن: باید "${w.includes}" ولري`;
        if (w.format === "regex")
          return `ناسم متن: باید د ${w.pattern} سره مطابقت ولري`;
        return `${Y[w.format] ?? z.format} ناسم دی`;
      }
      case "not_multiple_of":
        return `ناسم عدد: باید د ${z.divisor} مضرب وي`;
      case "unrecognized_keys":
        return `ناسم ${z.keys.length > 1 ? "کلیډونه" : "کلیډ"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `ناسم کلیډ په ${z.origin} کې`;
      case "invalid_union":
        return "ناسمه ورودي";
      case "invalid_element":
        return `ناسم عنصر په ${z.origin} کې`;
      default:
        return "ناسمه ورودي";
    }
  };
};
var W6A = E(() => {
  A3();
});
function Qu1() {
  return { localeError: bcq() };
}
var bcq = () => {
  let A = {
    string: { unit: "znaków", verb: "mieć" },
    file: { unit: "bajtów", verb: "mieć" },
    array: { unit: "elementów", verb: "mieć" },
    set: { unit: "elementów", verb: "mieć" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "liczba";
        case "object": {
          if (Array.isArray(z)) return "tablica";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "wyrażenie",
      email: "adres email",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "data i godzina w formacie ISO",
      date: "data w formacie ISO",
      time: "godzina w formacie ISO",
      duration: "czas trwania ISO",
      ipv4: "adres IPv4",
      ipv6: "adres IPv6",
      cidrv4: "zakres IPv4",
      cidrv6: "zakres IPv6",
      base64: "ciąg znaków zakodowany w formacie base64",
      base64url: "ciąg znaków zakodowany w formacie base64url",
      json_string: "ciąg znaków w formacie JSON",
      e164: "liczba E.164",
      jwt: "JWT",
      template_literal: "wejście",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Nieprawidłowe dane wejściowe: oczekiwano ${z.expected}, otrzymano ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Nieprawidłowe dane wejściowe: oczekiwano ${g7(z.values[0])}`;
        return `Nieprawidłowa opcja: oczekiwano jednej z wartości ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Za duża wartość: oczekiwano, że ${z.origin ?? "wartość"} będzie mieć ${w}${z.maximum.toString()} ${_.unit ?? "elementów"}`;
        return `Zbyt duż(y/a/e): oczekiwano, że ${z.origin ?? "wartość"} będzie wynosić ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Za mała wartość: oczekiwano, że ${z.origin ?? "wartość"} będzie mieć ${w}${z.minimum.toString()} ${_.unit ?? "elementów"}`;
        return `Zbyt mał(y/a/e): oczekiwano, że ${z.origin ?? "wartość"} będzie wynosić ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Nieprawidłowy ciąg znaków: musi zaczynać się od "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Nieprawidłowy ciąg znaków: musi kończyć się na "${w.suffix}"`;
        if (w.format === "includes")
          return `Nieprawidłowy ciąg znaków: musi zawierać "${w.includes}"`;
        if (w.format === "regex")
          return `Nieprawidłowy ciąg znaków: musi odpowiadać wzorcowi ${w.pattern}`;
        return `Nieprawidłow(y/a/e) ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Nieprawidłowa liczba: musi być wielokrotnością ${z.divisor}`;
      case "unrecognized_keys":
        return `Nierozpoznane klucze${z.keys.length > 1 ? "s" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Nieprawidłowy klucz w ${z.origin}`;
      case "invalid_union":
        return "Nieprawidłowe dane wejściowe";
      case "invalid_element":
        return `Nieprawidłowa wartość w ${z.origin}`;
      default:
        return "Nieprawidłowe dane wejściowe";
    }
  };
};
var G6A = E(() => {
  A3();
});
function Uu1() {
  return { localeError: ucq() };
}
var ucq = () => {
  let A = {
    string: { unit: "caracteres", verb: "ter" },
    file: { unit: "bytes", verb: "ter" },
    array: { unit: "itens", verb: "ter" },
    set: { unit: "itens", verb: "ter" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "número";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "nulo";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "padrão",
      email: "endereço de e-mail",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "data e hora ISO",
      date: "data ISO",
      time: "hora ISO",
      duration: "duração ISO",
      ipv4: "endereço IPv4",
      ipv6: "endereço IPv6",
      cidrv4: "faixa de IPv4",
      cidrv6: "faixa de IPv6",
      base64: "texto codificado em base64",
      base64url: "URL codificada em base64",
      json_string: "texto JSON",
      e164: "número E.164",
      jwt: "JWT",
      template_literal: "entrada",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Tipo inválido: esperado ${z.expected}, recebido ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Entrada inválida: esperado ${g7(z.values[0])}`;
        return `Opção inválida: esperada uma das ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Muito grande: esperado que ${z.origin ?? "valor"} tivesse ${w}${z.maximum.toString()} ${_.unit ?? "elementos"}`;
        return `Muito grande: esperado que ${z.origin ?? "valor"} fosse ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Muito pequeno: esperado que ${z.origin} tivesse ${w}${z.minimum.toString()} ${_.unit}`;
        return `Muito pequeno: esperado que ${z.origin} fosse ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Texto inválido: deve começar com "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Texto inválido: deve terminar com "${w.suffix}"`;
        if (w.format === "includes")
          return `Texto inválido: deve incluir "${w.includes}"`;
        if (w.format === "regex")
          return `Texto inválido: deve corresponder ao padrão ${w.pattern}`;
        return `${Y[w.format] ?? z.format} inválido`;
      }
      case "not_multiple_of":
        return `Número inválido: deve ser múltiplo de ${z.divisor}`;
      case "unrecognized_keys":
        return `Chave${z.keys.length > 1 ? "s" : ""} desconhecida${z.keys.length > 1 ? "s" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Chave inválida em ${z.origin}`;
      case "invalid_union":
        return "Entrada inválida";
      case "invalid_element":
        return `Valor inválido em ${z.origin}`;
      default:
        return "Campo inválido";
    }
  };
};
var Z6A = E(() => {
  A3();
});
function f6A(A, q, K, Y) {
  let z = Math.abs(A),
    w = z % 10,
    _ = z % 100;
  if (_ >= 11 && _ <= 19) return Y;
  if (w === 1) return q;
  if (w >= 2 && w <= 4) return K;
  return Y;
}
function du1() {
  return { localeError: mcq() };
}
var mcq = () => {
  let A = {
    string: {
      unit: { one: "символ", few: "символа", many: "символов" },
      verb: "иметь",
    },
    file: { unit: { one: "байт", few: "байта", many: "байт" }, verb: "иметь" },
    array: {
      unit: { one: "элемент", few: "элемента", many: "элементов" },
      verb: "иметь",
    },
    set: {
      unit: { one: "элемент", few: "элемента", many: "элементов" },
      verb: "иметь",
    },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "число";
        case "object": {
          if (Array.isArray(z)) return "массив";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "ввод",
      email: "email адрес",
      url: "URL",
      emoji: "эмодзи",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO дата и время",
      date: "ISO дата",
      time: "ISO время",
      duration: "ISO длительность",
      ipv4: "IPv4 адрес",
      ipv6: "IPv6 адрес",
      cidrv4: "IPv4 диапазон",
      cidrv6: "IPv6 диапазон",
      base64: "строка в формате base64",
      base64url: "строка в формате base64url",
      json_string: "JSON строка",
      e164: "номер E.164",
      jwt: "JWT",
      template_literal: "ввод",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Неверный ввод: ожидалось ${z.expected}, получено ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Неверный ввод: ожидалось ${g7(z.values[0])}`;
        return `Неверный вариант: ожидалось одно из ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_) {
          let $ = Number(z.maximum),
            O = f6A($, _.unit.one, _.unit.few, _.unit.many);
          return `Слишком большое значение: ожидалось, что ${z.origin ?? "значение"} будет иметь ${w}${z.maximum.toString()} ${O}`;
        }
        return `Слишком большое значение: ожидалось, что ${z.origin ?? "значение"} будет ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_) {
          let $ = Number(z.minimum),
            O = f6A($, _.unit.one, _.unit.few, _.unit.many);
          return `Слишком маленькое значение: ожидалось, что ${z.origin} будет иметь ${w}${z.minimum.toString()} ${O}`;
        }
        return `Слишком маленькое значение: ожидалось, что ${z.origin} будет ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Неверная строка: должна начинаться с "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Неверная строка: должна заканчиваться на "${w.suffix}"`;
        if (w.format === "includes")
          return `Неверная строка: должна содержать "${w.includes}"`;
        if (w.format === "regex")
          return `Неверная строка: должна соответствовать шаблону ${w.pattern}`;
        return `Неверный ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Неверное число: должно быть кратным ${z.divisor}`;
      case "unrecognized_keys":
        return `Нераспознанн${z.keys.length > 1 ? "ые" : "ый"} ключ${z.keys.length > 1 ? "и" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Неверный ключ в ${z.origin}`;
      case "invalid_union":
        return "Неверные входные данные";
      case "invalid_element":
        return `Неверное значение в ${z.origin}`;
      default:
        return "Неверные входные данные";
    }
  };
};
var T6A = E(() => {
  A3();
});
function cu1() {
  return { localeError: Bcq() };
}
var Bcq = () => {
  let A = {
    string: { unit: "znakov", verb: "imeti" },
    file: { unit: "bajtov", verb: "imeti" },
    array: { unit: "elementov", verb: "imeti" },
    set: { unit: "elementov", verb: "imeti" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "število";
        case "object": {
          if (Array.isArray(z)) return "tabela";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "vnos",
      email: "e-poštni naslov",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO datum in čas",
      date: "ISO datum",
      time: "ISO čas",
      duration: "ISO trajanje",
      ipv4: "IPv4 naslov",
      ipv6: "IPv6 naslov",
      cidrv4: "obseg IPv4",
      cidrv6: "obseg IPv6",
      base64: "base64 kodiran niz",
      base64url: "base64url kodiran niz",
      json_string: "JSON niz",
      e164: "E.164 številka",
      jwt: "JWT",
      template_literal: "vnos",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Neveljaven vnos: pričakovano ${z.expected}, prejeto ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Neveljaven vnos: pričakovano ${g7(z.values[0])}`;
        return `Neveljavna možnost: pričakovano eno izmed ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Preveliko: pričakovano, da bo ${z.origin ?? "vrednost"} imelo ${w}${z.maximum.toString()} ${_.unit ?? "elementov"}`;
        return `Preveliko: pričakovano, da bo ${z.origin ?? "vrednost"} ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Premajhno: pričakovano, da bo ${z.origin} imelo ${w}${z.minimum.toString()} ${_.unit}`;
        return `Premajhno: pričakovano, da bo ${z.origin} ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Neveljaven niz: mora se začeti z "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Neveljaven niz: mora se končati z "${w.suffix}"`;
        if (w.format === "includes")
          return `Neveljaven niz: mora vsebovati "${w.includes}"`;
        if (w.format === "regex")
          return `Neveljaven niz: mora ustrezati vzorcu ${w.pattern}`;
        return `Neveljaven ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Neveljavno število: mora biti večkratnik ${z.divisor}`;
      case "unrecognized_keys":
        return `Neprepoznan${z.keys.length > 1 ? "i ključi" : " ključ"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Neveljaven ključ v ${z.origin}`;
      case "invalid_union":
        return "Neveljaven vnos";
      case "invalid_element":
        return `Neveljavna vrednost v ${z.origin}`;
      default:
        return "Neveljaven vnos";
    }
  };
};
var N6A = E(() => {
  A3();
});
function lu1() {
  return { localeError: gcq() };
}
var gcq = () => {
  let A = {
    string: { unit: "tecken", verb: "att ha" },
    file: { unit: "bytes", verb: "att ha" },
    array: { unit: "objekt", verb: "att innehålla" },
    set: { unit: "objekt", verb: "att innehålla" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "antal";
        case "object": {
          if (Array.isArray(z)) return "lista";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "reguljärt uttryck",
      email: "e-postadress",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO-datum och tid",
      date: "ISO-datum",
      time: "ISO-tid",
      duration: "ISO-varaktighet",
      ipv4: "IPv4-intervall",
      ipv6: "IPv6-intervall",
      cidrv4: "IPv4-spektrum",
      cidrv6: "IPv6-spektrum",
      base64: "base64-kodad sträng",
      base64url: "base64url-kodad sträng",
      json_string: "JSON-sträng",
      e164: "E.164-nummer",
      jwt: "JWT",
      template_literal: "mall-literal",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Ogiltig inmatning: förväntat ${z.expected}, fick ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Ogiltig inmatning: förväntat ${g7(z.values[0])}`;
        return `Ogiltigt val: förväntade en av ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `För stor(t): förväntade ${z.origin ?? "värdet"} att ha ${w}${z.maximum.toString()} ${_.unit ?? "element"}`;
        return `För stor(t): förväntat ${z.origin ?? "värdet"} att ha ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `För lite(t): förväntade ${z.origin ?? "värdet"} att ha ${w}${z.minimum.toString()} ${_.unit}`;
        return `För lite(t): förväntade ${z.origin ?? "värdet"} att ha ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Ogiltig sträng: måste börja med "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Ogiltig sträng: måste sluta med "${w.suffix}"`;
        if (w.format === "includes")
          return `Ogiltig sträng: måste innehålla "${w.includes}"`;
        if (w.format === "regex")
          return `Ogiltig sträng: måste matcha mönstret "${w.pattern}"`;
        return `Ogiltig(t) ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Ogiltigt tal: måste vara en multipel av ${z.divisor}`;
      case "unrecognized_keys":
        return `${z.keys.length > 1 ? "Okända nycklar" : "Okänd nyckel"}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Ogiltig nyckel i ${z.origin ?? "värdet"}`;
      case "invalid_union":
        return "Ogiltig input";
      case "invalid_element":
        return `Ogiltigt värde i ${z.origin ?? "värdet"}`;
      default:
        return "Ogiltig input";
    }
  };
};
var V6A = E(() => {
  A3();
});
function iu1() {
  return { localeError: Fcq() };
}
var Fcq = () => {
  let A = {
    string: { unit: "எழுத்துக்கள்", verb: "கொண்டிருக்க வேண்டும்" },
    file: { unit: "பைட்டுகள்", verb: "கொண்டிருக்க வேண்டும்" },
    array: { unit: "உறுப்புகள்", verb: "கொண்டிருக்க வேண்டும்" },
    set: { unit: "உறுப்புகள்", verb: "கொண்டிருக்க வேண்டும்" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "எண் அல்லாதது" : "எண்";
        case "object": {
          if (Array.isArray(z)) return "அணி";
          if (z === null) return "வெறுமை";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "உள்ளீடு",
      email: "மின்னஞ்சல் முகவரி",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO தேதி நேரம்",
      date: "ISO தேதி",
      time: "ISO நேரம்",
      duration: "ISO கால அளவு",
      ipv4: "IPv4 முகவரி",
      ipv6: "IPv6 முகவரி",
      cidrv4: "IPv4 வரம்பு",
      cidrv6: "IPv6 வரம்பு",
      base64: "base64-encoded சரம்",
      base64url: "base64url-encoded சரம்",
      json_string: "JSON சரம்",
      e164: "E.164 எண்",
      jwt: "JWT",
      template_literal: "input",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `தவறான உள்ளீடு: எதிர்பார்க்கப்பட்டது ${z.expected}, பெறப்பட்டது ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `தவறான உள்ளீடு: எதிர்பார்க்கப்பட்டது ${g7(z.values[0])}`;
        return `தவறான விருப்பம்: எதிர்பார்க்கப்பட்டது ${XA(z.values, "|")} இல் ஒன்று`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `மிக பெரியது: எதிர்பார்க்கப்பட்டது ${z.origin ?? "மதிப்பு"} ${w}${z.maximum.toString()} ${_.unit ?? "உறுப்புகள்"} ஆக இருக்க வேண்டும்`;
        return `மிக பெரியது: எதிர்பார்க்கப்பட்டது ${z.origin ?? "மதிப்பு"} ${w}${z.maximum.toString()} ஆக இருக்க வேண்டும்`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `மிகச் சிறியது: எதிர்பார்க்கப்பட்டது ${z.origin} ${w}${z.minimum.toString()} ${_.unit} ஆக இருக்க வேண்டும்`;
        return `மிகச் சிறியது: எதிர்பார்க்கப்பட்டது ${z.origin} ${w}${z.minimum.toString()} ஆக இருக்க வேண்டும்`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `தவறான சரம்: "${w.prefix}" இல் தொடங்க வேண்டும்`;
        if (w.format === "ends_with")
          return `தவறான சரம்: "${w.suffix}" இல் முடிவடைய வேண்டும்`;
        if (w.format === "includes")
          return `தவறான சரம்: "${w.includes}" ஐ உள்ளடக்க வேண்டும்`;
        if (w.format === "regex")
          return `தவறான சரம்: ${w.pattern} முறைபாட்டுடன் பொருந்த வேண்டும்`;
        return `தவறான ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `தவறான எண்: ${z.divisor} இன் பலமாக இருக்க வேண்டும்`;
      case "unrecognized_keys":
        return `அடையாளம் தெரியாத விசை${z.keys.length > 1 ? "கள்" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `${z.origin} இல் தவறான விசை`;
      case "invalid_union":
        return "தவறான உள்ளீடு";
      case "invalid_element":
        return `${z.origin} இல் தவறான மதிப்பு`;
      default:
        return "தவறான உள்ளீடு";
    }
  };
};
var v6A = E(() => {
  A3();
});
function nu1() {
  return { localeError: pcq() };
}
var pcq = () => {
  let A = {
    string: { unit: "ตัวอักษร", verb: "ควรมี" },
    file: { unit: "ไบต์", verb: "ควรมี" },
    array: { unit: "รายการ", verb: "ควรมี" },
    set: { unit: "รายการ", verb: "ควรมี" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "ไม่ใช่ตัวเลข (NaN)" : "ตัวเลข";
        case "object": {
          if (Array.isArray(z)) return "อาร์เรย์ (Array)";
          if (z === null) return "ไม่มีค่า (null)";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "ข้อมูลที่ป้อน",
      email: "ที่อยู่อีเมล",
      url: "URL",
      emoji: "อิโมจิ",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "วันที่เวลาแบบ ISO",
      date: "วันที่แบบ ISO",
      time: "เวลาแบบ ISO",
      duration: "ช่วงเวลาแบบ ISO",
      ipv4: "ที่อยู่ IPv4",
      ipv6: "ที่อยู่ IPv6",
      cidrv4: "ช่วง IP แบบ IPv4",
      cidrv6: "ช่วง IP แบบ IPv6",
      base64: "ข้อความแบบ Base64",
      base64url: "ข้อความแบบ Base64 สำหรับ URL",
      json_string: "ข้อความแบบ JSON",
      e164: "เบอร์โทรศัพท์ระหว่างประเทศ (E.164)",
      jwt: "โทเคน JWT",
      template_literal: "ข้อมูลที่ป้อน",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `ประเภทข้อมูลไม่ถูกต้อง: ควรเป็น ${z.expected} แต่ได้รับ ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `ค่าไม่ถูกต้อง: ควรเป็น ${g7(z.values[0])}`;
        return `ตัวเลือกไม่ถูกต้อง: ควรเป็นหนึ่งใน ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "ไม่เกิน" : "น้อยกว่า",
          _ = q(z.origin);
        if (_)
          return `เกินกำหนด: ${z.origin ?? "ค่า"} ควรมี${w} ${z.maximum.toString()} ${_.unit ?? "รายการ"}`;
        return `เกินกำหนด: ${z.origin ?? "ค่า"} ควรมี${w} ${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? "อย่างน้อย" : "มากกว่า",
          _ = q(z.origin);
        if (_)
          return `น้อยกว่ากำหนด: ${z.origin} ควรมี${w} ${z.minimum.toString()} ${_.unit}`;
        return `น้อยกว่ากำหนด: ${z.origin} ควรมี${w} ${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `รูปแบบไม่ถูกต้อง: ข้อความต้องขึ้นต้นด้วย "${w.prefix}"`;
        if (w.format === "ends_with")
          return `รูปแบบไม่ถูกต้อง: ข้อความต้องลงท้ายด้วย "${w.suffix}"`;
        if (w.format === "includes")
          return `รูปแบบไม่ถูกต้อง: ข้อความต้องมี "${w.includes}" อยู่ในข้อความ`;
        if (w.format === "regex")
          return `รูปแบบไม่ถูกต้อง: ต้องตรงกับรูปแบบที่กำหนด ${w.pattern}`;
        return `รูปแบบไม่ถูกต้อง: ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `ตัวเลขไม่ถูกต้อง: ต้องเป็นจำนวนที่หารด้วย ${z.divisor} ได้ลงตัว`;
      case "unrecognized_keys":
        return `พบคีย์ที่ไม่รู้จัก: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `คีย์ไม่ถูกต้องใน ${z.origin}`;
      case "invalid_union":
        return "ข้อมูลไม่ถูกต้อง: ไม่ตรงกับรูปแบบยูเนียนที่กำหนดไว้";
      case "invalid_element":
        return `ข้อมูลไม่ถูกต้องใน ${z.origin}`;
      default:
        return "ข้อมูลไม่ถูกต้อง";
    }
  };
};
var k6A = E(() => {
  A3();
});
function ru1() {
  return { localeError: Ucq() };
}
var Qcq = (A) => {
    let q = typeof A;
    switch (q) {
      case "number":
        return Number.isNaN(A) ? "NaN" : "number";
      case "object": {
        if (Array.isArray(A)) return "array";
        if (A === null) return "null";
        if (Object.getPrototypeOf(A) !== Object.prototype && A.constructor)
          return A.constructor.name;
      }
    }
    return q;
  },
  Ucq = () => {
    let A = {
      string: { unit: "karakter", verb: "olmalı" },
      file: { unit: "bayt", verb: "olmalı" },
      array: { unit: "öğe", verb: "olmalı" },
      set: { unit: "öğe", verb: "olmalı" },
    };
    function q(Y) {
      return A[Y] ?? null;
    }
    let K = {
      regex: "girdi",
      email: "e-posta adresi",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO tarih ve saat",
      date: "ISO tarih",
      time: "ISO saat",
      duration: "ISO süre",
      ipv4: "IPv4 adresi",
      ipv6: "IPv6 adresi",
      cidrv4: "IPv4 aralığı",
      cidrv6: "IPv6 aralığı",
      base64: "base64 ile şifrelenmiş metin",
      base64url: "base64url ile şifrelenmiş metin",
      json_string: "JSON dizesi",
      e164: "E.164 sayısı",
      jwt: "JWT",
      template_literal: "Şablon dizesi",
    };
    return (Y) => {
      switch (Y.code) {
        case "invalid_type":
          return `Geçersiz değer: beklenen ${Y.expected}, alınan ${Qcq(Y.input)}`;
        case "invalid_value":
          if (Y.values.length === 1)
            return `Geçersiz değer: beklenen ${g7(Y.values[0])}`;
          return `Geçersiz seçenek: aşağıdakilerden biri olmalı: ${XA(Y.values, "|")}`;
        case "too_big": {
          let z = Y.inclusive ? "<=" : "<",
            w = q(Y.origin);
          if (w)
            return `Çok büyük: beklenen ${Y.origin ?? "değer"} ${z}${Y.maximum.toString()} ${w.unit ?? "öğe"}`;
          return `Çok büyük: beklenen ${Y.origin ?? "değer"} ${z}${Y.maximum.toString()}`;
        }
        case "too_small": {
          let z = Y.inclusive ? ">=" : ">",
            w = q(Y.origin);
          if (w)
            return `Çok küçük: beklenen ${Y.origin} ${z}${Y.minimum.toString()} ${w.unit}`;
          return `Çok küçük: beklenen ${Y.origin} ${z}${Y.minimum.toString()}`;
        }
        case "invalid_format": {
          let z = Y;
          if (z.format === "starts_with")
            return `Geçersiz metin: "${z.prefix}" ile başlamalı`;
          if (z.format === "ends_with")
            return `Geçersiz metin: "${z.suffix}" ile bitmeli`;
          if (z.format === "includes")
            return `Geçersiz metin: "${z.includes}" içermeli`;
          if (z.format === "regex")
            return `Geçersiz metin: ${z.pattern} desenine uymalı`;
          return `Geçersiz ${K[z.format] ?? Y.format}`;
        }
        case "not_multiple_of":
          return `Geçersiz sayı: ${Y.divisor} ile tam bölünebilmeli`;
        case "unrecognized_keys":
          return `Tanınmayan anahtar${Y.keys.length > 1 ? "lar" : ""}: ${XA(Y.keys, ", ")}`;
        case "invalid_key":
          return `${Y.origin} içinde geçersiz anahtar`;
        case "invalid_union":
          return "Geçersiz değer";
        case "invalid_element":
          return `${Y.origin} içinde geçersiz değer`;
        default:
          return "Geçersiz değer";
      }
    };
  };
var E6A = E(() => {
  A3();
});
function ou1() {
  return { localeError: dcq() };
}
var dcq = () => {
  let A = {
    string: { unit: "символів", verb: "матиме" },
    file: { unit: "байтів", verb: "матиме" },
    array: { unit: "елементів", verb: "матиме" },
    set: { unit: "елементів", verb: "матиме" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "число";
        case "object": {
          if (Array.isArray(z)) return "масив";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "вхідні дані",
      email: "адреса електронної пошти",
      url: "URL",
      emoji: "емодзі",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "дата та час ISO",
      date: "дата ISO",
      time: "час ISO",
      duration: "тривалість ISO",
      ipv4: "адреса IPv4",
      ipv6: "адреса IPv6",
      cidrv4: "діапазон IPv4",
      cidrv6: "діапазон IPv6",
      base64: "рядок у кодуванні base64",
      base64url: "рядок у кодуванні base64url",
      json_string: "рядок JSON",
      e164: "номер E.164",
      jwt: "JWT",
      template_literal: "вхідні дані",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Неправильні вхідні дані: очікується ${z.expected}, отримано ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Неправильні вхідні дані: очікується ${g7(z.values[0])}`;
        return `Неправильна опція: очікується одне з ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Занадто велике: очікується, що ${z.origin ?? "значення"} ${_.verb} ${w}${z.maximum.toString()} ${_.unit ?? "елементів"}`;
        return `Занадто велике: очікується, що ${z.origin ?? "значення"} буде ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Занадто мале: очікується, що ${z.origin} ${_.verb} ${w}${z.minimum.toString()} ${_.unit}`;
        return `Занадто мале: очікується, що ${z.origin} буде ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Неправильний рядок: повинен починатися з "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Неправильний рядок: повинен закінчуватися на "${w.suffix}"`;
        if (w.format === "includes")
          return `Неправильний рядок: повинен містити "${w.includes}"`;
        if (w.format === "regex")
          return `Неправильний рядок: повинен відповідати шаблону ${w.pattern}`;
        return `Неправильний ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `Неправильне число: повинно бути кратним ${z.divisor}`;
      case "unrecognized_keys":
        return `Нерозпізнаний ключ${z.keys.length > 1 ? "і" : ""}: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Неправильний ключ у ${z.origin}`;
      case "invalid_union":
        return "Неправильні вхідні дані";
      case "invalid_element":
        return `Неправильне значення у ${z.origin}`;
      default:
        return "Неправильні вхідні дані";
    }
  };
};
var L6A = E(() => {
  A3();
});
function au1() {
  return { localeError: ccq() };
}
var ccq = () => {
  let A = {
    string: { unit: "حروف", verb: "ہونا" },
    file: { unit: "بائٹس", verb: "ہونا" },
    array: { unit: "آئٹمز", verb: "ہونا" },
    set: { unit: "آئٹمز", verb: "ہونا" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "نمبر";
        case "object": {
          if (Array.isArray(z)) return "آرے";
          if (z === null) return "نل";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "ان پٹ",
      email: "ای میل ایڈریس",
      url: "یو آر ایل",
      emoji: "ایموجی",
      uuid: "یو یو آئی ڈی",
      uuidv4: "یو یو آئی ڈی وی 4",
      uuidv6: "یو یو آئی ڈی وی 6",
      nanoid: "نینو آئی ڈی",
      guid: "جی یو آئی ڈی",
      cuid: "سی یو آئی ڈی",
      cuid2: "سی یو آئی ڈی 2",
      ulid: "یو ایل آئی ڈی",
      xid: "ایکس آئی ڈی",
      ksuid: "کے ایس یو آئی ڈی",
      datetime: "آئی ایس او ڈیٹ ٹائم",
      date: "آئی ایس او تاریخ",
      time: "آئی ایس او وقت",
      duration: "آئی ایس او مدت",
      ipv4: "آئی پی وی 4 ایڈریس",
      ipv6: "آئی پی وی 6 ایڈریس",
      cidrv4: "آئی پی وی 4 رینج",
      cidrv6: "آئی پی وی 6 رینج",
      base64: "بیس 64 ان کوڈڈ سٹرنگ",
      base64url: "بیس 64 یو آر ایل ان کوڈڈ سٹرنگ",
      json_string: "جے ایس او این سٹرنگ",
      e164: "ای 164 نمبر",
      jwt: "جے ڈبلیو ٹی",
      template_literal: "ان پٹ",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `غلط ان پٹ: ${z.expected} متوقع تھا، ${K(z.input)} موصول ہوا`;
      case "invalid_value":
        if (z.values.length === 1)
          return `غلط ان پٹ: ${g7(z.values[0])} متوقع تھا`;
        return `غلط آپشن: ${XA(z.values, "|")} میں سے ایک متوقع تھا`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `بہت بڑا: ${z.origin ?? "ویلیو"} کے ${w}${z.maximum.toString()} ${_.unit ?? "عناصر"} ہونے متوقع تھے`;
        return `بہت بڑا: ${z.origin ?? "ویلیو"} کا ${w}${z.maximum.toString()} ہونا متوقع تھا`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `بہت چھوٹا: ${z.origin} کے ${w}${z.minimum.toString()} ${_.unit} ہونے متوقع تھے`;
        return `بہت چھوٹا: ${z.origin} کا ${w}${z.minimum.toString()} ہونا متوقع تھا`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `غلط سٹرنگ: "${w.prefix}" سے شروع ہونا چاہیے`;
        if (w.format === "ends_with")
          return `غلط سٹرنگ: "${w.suffix}" پر ختم ہونا چاہیے`;
        if (w.format === "includes")
          return `غلط سٹرنگ: "${w.includes}" شامل ہونا چاہیے`;
        if (w.format === "regex")
          return `غلط سٹرنگ: پیٹرن ${w.pattern} سے میچ ہونا چاہیے`;
        return `غلط ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `غلط نمبر: ${z.divisor} کا مضاعف ہونا چاہیے`;
      case "unrecognized_keys":
        return `غیر تسلیم شدہ کی${z.keys.length > 1 ? "ز" : ""}: ${XA(z.keys, "، ")}`;
      case "invalid_key":
        return `${z.origin} میں غلط کی`;
      case "invalid_union":
        return "غلط ان پٹ";
      case "invalid_element":
        return `${z.origin} میں غلط ویلیو`;
      default:
        return "غلط ان پٹ";
    }
  };
};
var y6A = E(() => {
  A3();
});
function su1() {
  return { localeError: lcq() };
}
var lcq = () => {
  let A = {
    string: { unit: "ký tự", verb: "có" },
    file: { unit: "byte", verb: "có" },
    array: { unit: "phần tử", verb: "có" },
    set: { unit: "phần tử", verb: "có" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "số";
        case "object": {
          if (Array.isArray(z)) return "mảng";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "đầu vào",
      email: "địa chỉ email",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ngày giờ ISO",
      date: "ngày ISO",
      time: "giờ ISO",
      duration: "khoảng thời gian ISO",
      ipv4: "địa chỉ IPv4",
      ipv6: "địa chỉ IPv6",
      cidrv4: "dải IPv4",
      cidrv6: "dải IPv6",
      base64: "chuỗi mã hóa base64",
      base64url: "chuỗi mã hóa base64url",
      json_string: "chuỗi JSON",
      e164: "số E.164",
      jwt: "JWT",
      template_literal: "đầu vào",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `Đầu vào không hợp lệ: mong đợi ${z.expected}, nhận được ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `Đầu vào không hợp lệ: mong đợi ${g7(z.values[0])}`;
        return `Tùy chọn không hợp lệ: mong đợi một trong các giá trị ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `Quá lớn: mong đợi ${z.origin ?? "giá trị"} ${_.verb} ${w}${z.maximum.toString()} ${_.unit ?? "phần tử"}`;
        return `Quá lớn: mong đợi ${z.origin ?? "giá trị"} ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `Quá nhỏ: mong đợi ${z.origin} ${_.verb} ${w}${z.minimum.toString()} ${_.unit}`;
        return `Quá nhỏ: mong đợi ${z.origin} ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `Chuỗi không hợp lệ: phải bắt đầu bằng "${w.prefix}"`;
        if (w.format === "ends_with")
          return `Chuỗi không hợp lệ: phải kết thúc bằng "${w.suffix}"`;
        if (w.format === "includes")
          return `Chuỗi không hợp lệ: phải bao gồm "${w.includes}"`;
        if (w.format === "regex")
          return `Chuỗi không hợp lệ: phải khớp với mẫu ${w.pattern}`;
        return `${Y[w.format] ?? z.format} không hợp lệ`;
      }
      case "not_multiple_of":
        return `Số không hợp lệ: phải là bội số của ${z.divisor}`;
      case "unrecognized_keys":
        return `Khóa không được nhận dạng: ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `Khóa không hợp lệ trong ${z.origin}`;
      case "invalid_union":
        return "Đầu vào không hợp lệ";
      case "invalid_element":
        return `Giá trị không hợp lệ trong ${z.origin}`;
      default:
        return "Đầu vào không hợp lệ";
    }
  };
};
var R6A = E(() => {
  A3();
});
function tu1() {
  return { localeError: icq() };
}
var icq = () => {
  let A = {
    string: { unit: "字符", verb: "包含" },
    file: { unit: "字节", verb: "包含" },
    array: { unit: "项", verb: "包含" },
    set: { unit: "项", verb: "包含" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "非数字(NaN)" : "数字";
        case "object": {
          if (Array.isArray(z)) return "数组";
          if (z === null) return "空值(null)";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "输入",
      email: "电子邮件",
      url: "URL",
      emoji: "表情符号",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO日期时间",
      date: "ISO日期",
      time: "ISO时间",
      duration: "ISO时长",
      ipv4: "IPv4地址",
      ipv6: "IPv6地址",
      cidrv4: "IPv4网段",
      cidrv6: "IPv6网段",
      base64: "base64编码字符串",
      base64url: "base64url编码字符串",
      json_string: "JSON字符串",
      e164: "E.164号码",
      jwt: "JWT",
      template_literal: "输入",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `无效输入：期望 ${z.expected}，实际接收 ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1) return `无效输入：期望 ${g7(z.values[0])}`;
        return `无效选项：期望以下之一 ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `数值过大：期望 ${z.origin ?? "值"} ${w}${z.maximum.toString()} ${_.unit ?? "个元素"}`;
        return `数值过大：期望 ${z.origin ?? "值"} ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `数值过小：期望 ${z.origin} ${w}${z.minimum.toString()} ${_.unit}`;
        return `数值过小：期望 ${z.origin} ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `无效字符串：必须以 "${w.prefix}" 开头`;
        if (w.format === "ends_with")
          return `无效字符串：必须以 "${w.suffix}" 结尾`;
        if (w.format === "includes")
          return `无效字符串：必须包含 "${w.includes}"`;
        if (w.format === "regex")
          return `无效字符串：必须满足正则表达式 ${w.pattern}`;
        return `无效${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `无效数字：必须是 ${z.divisor} 的倍数`;
      case "unrecognized_keys":
        return `出现未知的键(key): ${XA(z.keys, ", ")}`;
      case "invalid_key":
        return `${z.origin} 中的键(key)无效`;
      case "invalid_union":
        return "无效输入";
      case "invalid_element":
        return `${z.origin} 中包含无效值(value)`;
      default:
        return "无效输入";
    }
  };
};
var C6A = E(() => {
  A3();
});
function eu1() {
  return { localeError: ncq() };
}
var ncq = () => {
  let A = {
    string: { unit: "字元", verb: "擁有" },
    file: { unit: "位元組", verb: "擁有" },
    array: { unit: "項目", verb: "擁有" },
    set: { unit: "項目", verb: "擁有" },
  };
  function q(z) {
    return A[z] ?? null;
  }
  let K = (z) => {
      let w = typeof z;
      switch (w) {
        case "number":
          return Number.isNaN(z) ? "NaN" : "number";
        case "object": {
          if (Array.isArray(z)) return "array";
          if (z === null) return "null";
          if (Object.getPrototypeOf(z) !== Object.prototype && z.constructor)
            return z.constructor.name;
        }
      }
      return w;
    },
    Y = {
      regex: "輸入",
      email: "郵件地址",
      url: "URL",
      emoji: "emoji",
      uuid: "UUID",
      uuidv4: "UUIDv4",
      uuidv6: "UUIDv6",
      nanoid: "nanoid",
      guid: "GUID",
      cuid: "cuid",
      cuid2: "cuid2",
      ulid: "ULID",
      xid: "XID",
      ksuid: "KSUID",
      datetime: "ISO 日期時間",
      date: "ISO 日期",
      time: "ISO 時間",
      duration: "ISO 期間",
      ipv4: "IPv4 位址",
      ipv6: "IPv6 位址",
      cidrv4: "IPv4 範圍",
      cidrv6: "IPv6 範圍",
      base64: "base64 編碼字串",
      base64url: "base64url 編碼字串",
      json_string: "JSON 字串",
      e164: "E.164 數值",
      jwt: "JWT",
      template_literal: "輸入",
    };
  return (z) => {
    switch (z.code) {
      case "invalid_type":
        return `無效的輸入值：預期為 ${z.expected}，但收到 ${K(z.input)}`;
      case "invalid_value":
        if (z.values.length === 1)
          return `無效的輸入值：預期為 ${g7(z.values[0])}`;
        return `無效的選項：預期為以下其中之一 ${XA(z.values, "|")}`;
      case "too_big": {
        let w = z.inclusive ? "<=" : "<",
          _ = q(z.origin);
        if (_)
          return `數值過大：預期 ${z.origin ?? "值"} 應為 ${w}${z.maximum.toString()} ${_.unit ?? "個元素"}`;
        return `數值過大：預期 ${z.origin ?? "值"} 應為 ${w}${z.maximum.toString()}`;
      }
      case "too_small": {
        let w = z.inclusive ? ">=" : ">",
          _ = q(z.origin);
        if (_)
          return `數值過小：預期 ${z.origin} 應為 ${w}${z.minimum.toString()} ${_.unit}`;
        return `數值過小：預期 ${z.origin} 應為 ${w}${z.minimum.toString()}`;
      }
      case "invalid_format": {
        let w = z;
        if (w.format === "starts_with")
          return `無效的字串：必須以 "${w.prefix}" 開頭`;
        if (w.format === "ends_with")
          return `無效的字串：必須以 "${w.suffix}" 結尾`;
        if (w.format === "includes")
          return `無效的字串：必須包含 "${w.includes}"`;
        if (w.format === "regex")
          return `無效的字串：必須符合格式 ${w.pattern}`;
        return `無效的 ${Y[w.format] ?? z.format}`;
      }
      case "not_multiple_of":
        return `無效的數字：必須為 ${z.divisor} 的倍數`;
      case "unrecognized_keys":
        return `無法識別的鍵值${z.keys.length > 1 ? "們" : ""}：${XA(z.keys, "、")}`;
      case "invalid_key":
        return `${z.origin} 中有無效的鍵值`;
      case "invalid_union":
        return "無效的輸入值";
      case "invalid_element":
        return `${z.origin} 中有無效的值`;
      default:
        return "無效的輸入值";
    }
  };
};
var S6A = E(() => {
  A3();
});
var H$6 = {};
s1(H$6, {
  zhTW: () => eu1,
  zhCN: () => tu1,
  vi: () => su1,
  ur: () => au1,
  ua: () => ou1,
  tr: () => ru1,
  th: () => nu1,
  ta: () => iu1,
  sv: () => lu1,
  sl: () => cu1,
  ru: () => du1,
  pt: () => Uu1,
  ps: () => pu1,
  pl: () => Qu1,
  ota: () => Fu1,
  no: () => gu1,
  nl: () => Bu1,
  ms: () => mu1,
  mk: () => uu1,
  ko: () => bu1,
  kh: () => xu1,
  ja: () => Iu1,
  it: () => hu1,
  id: () => Su1,
  hu: () => Cu1,
  he: () => Ru1,
  frCA: () => yu1,
  fr: () => Lu1,
  fi: () => Eu1,
  fa: () => ku1,
  es: () => vu1,
  eo: () => Vu1,
  en: () => XE6,
  de: () => Tu1,
  cs: () => fu1,
  ca: () => Zu1,
  be: () => Gu1,
  az: () => Wu1,
  ar: () => Pu1,
});
var rs6 = E(() => {
  le8();
  ie8();
  re8();
  oe8();
  ae8();
  se8();
  Nu1();
  te8();
  ee8();
  A6A();
  q6A();
  K6A();
  Y6A();
  z6A();
  w6A();
  _6A();
  $6A();
  O6A();
  H6A();
  j6A();
  J6A();
  D6A();
  X6A();
  M6A();
  P6A();
  W6A();
  G6A();
  Z6A();
  T6A();
  N6A();
  V6A();
  v6A();
  k6A();
  E6A();
  L6A();
  y6A();
  R6A();
  C6A();
  S6A();
});
class ME6 {
  constructor() {
    ((this._map = new WeakMap()), (this._idmap = new Map()));
  }
  add(A, ...q) {
    let K = q[0];
    if ((this._map.set(A, K), K && typeof K === "object" && "id" in K)) {
      if (this._idmap.has(K.id))
        throw Error(`ID ${K.id} already exists in the registry`);
      this._idmap.set(K.id, A);
    }
    return this;
  }
  remove(A) {
    return (this._map.delete(A), this);
  }
  get(A) {
    let q = A._zod.parent;
    if (q) {
      let K = { ...(this.get(q) ?? {}) };
      return (delete K.id, { ...K, ...this._map.get(A) });
    }
    return this._map.get(A);
  }
  has(A) {
    return this._map.has(A);
  }
}
function os6() {
  return new ME6();
}
var Am1, qm1, Ku;
var Km1 = E(() => {
  ((Am1 = Symbol("ZodOutput")), (qm1 = Symbol("ZodInput")));
  Ku = os6();
});
function Ym1(A, q) {
  return new A({ type: "string", ...V7(q) });
}
function zm1(A, q) {
  return new A({ type: "string", coerce: !0, ...V7(q) });
}
function as6(A, q) {
  return new A({
    type: "string",
    format: "email",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function PE6(A, q) {
  return new A({
    type: "string",
    format: "guid",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function ss6(A, q) {
  return new A({
    type: "string",
    format: "uuid",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function ts6(A, q) {
  return new A({
    type: "string",
    format: "uuid",
    check: "string_format",
    abort: !1,
    version: "v4",
    ...V7(q),
  });
}
function es6(A, q) {
  return new A({
    type: "string",
    format: "uuid",
    check: "string_format",
    abort: !1,
    version: "v6",
    ...V7(q),
  });
}
function At6(A, q) {
  return new A({
    type: "string",
    format: "uuid",
    check: "string_format",
    abort: !1,
    version: "v7",
    ...V7(q),
  });
}
function qt6(A, q) {
  return new A({
    type: "string",
    format: "url",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Kt6(A, q) {
  return new A({
    type: "string",
    format: "emoji",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Yt6(A, q) {
  return new A({
    type: "string",
    format: "nanoid",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function zt6(A, q) {
  return new A({
    type: "string",
    format: "cuid",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function wt6(A, q) {
  return new A({
    type: "string",
    format: "cuid2",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function _t6(A, q) {
  return new A({
    type: "string",
    format: "ulid",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function $t6(A, q) {
  return new A({
    type: "string",
    format: "xid",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Ot6(A, q) {
  return new A({
    type: "string",
    format: "ksuid",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Ht6(A, q) {
  return new A({
    type: "string",
    format: "ipv4",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function jt6(A, q) {
  return new A({
    type: "string",
    format: "ipv6",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Jt6(A, q) {
  return new A({
    type: "string",
    format: "cidrv4",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Dt6(A, q) {
  return new A({
    type: "string",
    format: "cidrv6",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Xt6(A, q) {
  return new A({
    type: "string",
    format: "base64",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Mt6(A, q) {
  return new A({
    type: "string",
    format: "base64url",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Pt6(A, q) {
  return new A({
    type: "string",
    format: "e164",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function Wt6(A, q) {
  return new A({
    type: "string",
    format: "jwt",
    check: "string_format",
    abort: !1,
    ...V7(q),
  });
}
function _m1(A, q) {
  return new A({
    type: "string",
    format: "datetime",
    check: "string_format",
    offset: !1,
    local: !1,
    precision: null,
    ...V7(q),
  });
}
function $m1(A, q) {
  return new A({
    type: "string",
    format: "date",
    check: "string_format",
    ...V7(q),
  });
}
function Om1(A, q) {
  return new A({
    type: "string",
    format: "time",
    check: "string_format",
    precision: null,
    ...V7(q),
  });
}
function Hm1(A, q) {
  return new A({
    type: "string",
    format: "duration",
    check: "string_format",
    ...V7(q),
  });
}
function jm1(A, q) {
  return new A({ type: "number", checks: [], ...V7(q) });
}
function Jm1(A, q) {
  return new A({ type: "number", coerce: !0, checks: [], ...V7(q) });
}
function Dm1(A, q) {
  return new A({
    type: "number",
    check: "number_format",
    abort: !1,
    format: "safeint",
    ...V7(q),
  });
}
function Xm1(A, q) {
  return new A({
    type: "number",
    check: "number_format",
    abort: !1,
    format: "float32",
    ...V7(q),
  });
}
function Mm1(A, q) {
  return new A({
    type: "number",
    check: "number_format",
    abort: !1,
    format: "float64",
    ...V7(q),
  });
}
function Pm1(A, q) {
  return new A({
    type: "number",
    check: "number_format",
    abort: !1,
    format: "int32",
    ...V7(q),
  });
}
function Wm1(A, q) {
  return new A({
    type: "number",
    check: "number_format",
    abort: !1,
    format: "uint32",
    ...V7(q),
  });
}
function Gm1(A, q) {
  return new A({ type: "boolean", ...V7(q) });
}
function Zm1(A, q) {
  return new A({ type: "boolean", coerce: !0, ...V7(q) });
}
function fm1(A, q) {
  return new A({ type: "bigint", ...V7(q) });
}
function Tm1(A, q) {
  return new A({ type: "bigint", coerce: !0, ...V7(q) });
}
function Nm1(A, q) {
  return new A({
    type: "bigint",
    check: "bigint_format",
    abort: !1,
    format: "int64",
    ...V7(q),
  });
}
function Vm1(A, q) {
  return new A({
    type: "bigint",
    check: "bigint_format",
    abort: !1,
    format: "uint64",
    ...V7(q),
  });
}
function vm1(A, q) {
  return new A({ type: "symbol", ...V7(q) });
}
function km1(A, q) {
  return new A({ type: "undefined", ...V7(q) });
}
function Em1(A, q) {
  return new A({ type: "null", ...V7(q) });
}
function Lm1(A) {
  return new A({ type: "any" });
}
function j$6(A) {
  return new A({ type: "unknown" });
}
function ym1(A, q) {
  return new A({ type: "never", ...V7(q) });
}
function Rm1(A, q) {
  return new A({ type: "void", ...V7(q) });
}
function Cm1(A, q) {
  return new A({ type: "date", ...V7(q) });
}
function Sm1(A, q) {
  return new A({ type: "date", coerce: !0, ...V7(q) });
}
function hm1(A, q) {
  return new A({ type: "nan", ...V7(q) });
}
function qQ(A, q) {
  return new Fs6({ check: "less_than", ...V7(q), value: A, inclusive: !1 });
}
function ZL(A, q) {
  return new Fs6({ check: "less_than", ...V7(q), value: A, inclusive: !0 });
}
function KQ(A, q) {
  return new ps6({ check: "greater_than", ...V7(q), value: A, inclusive: !1 });
}
function gT(A, q) {
  return new ps6({ check: "greater_than", ...V7(q), value: A, inclusive: !0 });
}
function Im1(A) {
  return KQ(0, A);
}
function xm1(A) {
  return qQ(0, A);
}
function bm1(A) {
  return ZL(0, A);
}
function um1(A) {
  return gT(0, A);
}
function sA6(A, q) {
  return new ix1({ check: "multiple_of", ...V7(q), value: A });
}
function J$6(A, q) {
  return new ox1({ check: "max_size", ...V7(q), maximum: A });
}
function tA6(A, q) {
  return new ax1({ check: "min_size", ...V7(q), minimum: A });
}
function WE6(A, q) {
  return new sx1({ check: "size_equals", ...V7(q), size: A });
}
function D$6(A, q) {
  return new tx1({ check: "max_length", ...V7(q), maximum: A });
}
function qr(A, q) {
  return new ex1({ check: "min_length", ...V7(q), minimum: A });
}
function X$6(A, q) {
  return new Ab1({ check: "length_equals", ...V7(q), length: A });
}
function GE6(A, q) {
  return new qb1({
    check: "string_format",
    format: "regex",
    ...V7(q),
    pattern: A,
  });
}
function ZE6(A) {
  return new Kb1({ check: "string_format", format: "lowercase", ...V7(A) });
}
function fE6(A) {
  return new Yb1({ check: "string_format", format: "uppercase", ...V7(A) });
}
function TE6(A, q) {
  return new zb1({
    check: "string_format",
    format: "includes",
    ...V7(q),
    includes: A,
  });
}
function NE6(A, q) {
  return new wb1({
    check: "string_format",
    format: "starts_with",
    ...V7(q),
    prefix: A,
  });
}
function VE6(A, q) {
  return new _b1({
    check: "string_format",
    format: "ends_with",
    ...V7(q),
    suffix: A,
  });
}
function mm1(A, q, K) {
  return new $b1({ check: "property", property: A, schema: q, ...V7(K) });
}
function vE6(A, q) {
  return new Ob1({ check: "mime_type", mime: A, ...V7(q) });
}
function YQ(A) {
  return new Hb1({ check: "overwrite", tx: A });
}
function kE6(A) {
  return YQ((q) => q.normalize(A));
}
function EE6() {
  return YQ((A) => A.trim());
}
function LE6() {
  return YQ((A) => A.toLowerCase());
}
function yE6() {
  return YQ((A) => A.toUpperCase());
}
function RE6(A, q, K) {
  return new A({ type: "array", element: q, ...V7(K) });
}
function rcq(A, q, K) {
  return new A({ type: "union", options: q, ...V7(K) });
}
function ocq(A, q, K, Y) {
  return new A({ type: "union", options: K, discriminator: q, ...V7(Y) });
}
function acq(A, q, K) {
  return new A({ type: "intersection", left: q, right: K });
}
function Bm1(A, q, K, Y) {
  let z = K instanceof T3;
  return new A({
    type: "tuple",
    items: q,
    rest: z ? K : null,
    ...V7(z ? Y : K),
  });
}
function scq(A, q, K, Y) {
  return new A({ type: "record", keyType: q, valueType: K, ...V7(Y) });
}
function tcq(A, q, K, Y) {
  return new A({ type: "map", keyType: q, valueType: K, ...V7(Y) });
}
function ecq(A, q, K) {
  return new A({ type: "set", valueType: q, ...V7(K) });
}
function Alq(A, q, K) {
  let Y = Array.isArray(q) ? Object.fromEntries(q.map((z) => [z, z])) : q;
  return new A({ type: "enum", entries: Y, ...V7(K) });
}
function qlq(A, q, K) {
  return new A({ type: "enum", entries: q, ...V7(K) });
}
function Klq(A, q, K) {
  return new A({
    type: "literal",
    values: Array.isArray(q) ? q : [q],
    ...V7(K),
  });
}
function gm1(A, q) {
  return new A({ type: "file", ...V7(q) });
}
function Ylq(A, q) {
  return new A({ type: "transform", transform: q });
}
function zlq(A, q) {
  return new A({ type: "optional", innerType: q });
}
function wlq(A, q) {
  return new A({ type: "nullable", innerType: q });
}
function _lq(A, q, K) {
  return new A({
    type: "default",
    innerType: q,
    get defaultValue() {
      return typeof K === "function" ? K() : K;
    },
  });
}
function $lq(A, q, K) {
  return new A({ type: "nonoptional", innerType: q, ...V7(K) });
}
function Olq(A, q) {
  return new A({ type: "success", innerType: q });
}
function Hlq(A, q, K) {
  return new A({
    type: "catch",
    innerType: q,
    catchValue: typeof K === "function" ? K : () => K,
  });
}
function jlq(A, q, K) {
  return new A({ type: "pipe", in: q, out: K });
}
function Jlq(A, q) {
  return new A({ type: "readonly", innerType: q });
}
function Dlq(A, q, K) {
  return new A({ type: "template_literal", parts: q, ...V7(K) });
}
function Xlq(A, q) {
  return new A({ type: "lazy", getter: q });
}
function Mlq(A, q) {
  return new A({ type: "promise", innerType: q });
}
function Fm1(A, q, K) {
  let Y = V7(K);
  return (
    Y.abort ?? (Y.abort = !0),
    new A({ type: "custom", check: "custom", fn: q, ...Y })
  );
}
function pm1(A, q, K) {
  return new A({ type: "custom", check: "custom", fn: q, ...V7(K) });
}
function Qm1(A, q) {
  let K = V7(q),
    Y = K.truthy ?? ["true", "1", "yes", "on", "y", "enabled"],
    z = K.falsy ?? ["false", "0", "no", "off", "n", "disabled"];
  if (K.case !== "sensitive")
    ((Y = Y.map((M) => (typeof M === "string" ? M.toLowerCase() : M))),
      (z = z.map((M) => (typeof M === "string" ? M.toLowerCase() : M))));
  let w = new Set(Y),
    _ = new Set(z),
    $ = A.Pipe ?? JE6,
    O = A.Boolean ?? OE6,
    H = A.String ?? oA6,
    J = new (A.Transform ?? jE6)({
      type: "transform",
      transform: (M, P) => {
        let W = M;
        if (K.case !== "sensitive") W = W.toLowerCase();
        if (w.has(W)) return !0;
        else if (_.has(W)) return !1;
        else
          return (
            P.issues.push({
              code: "invalid_value",
              expected: "stringbool",
              values: [...w, ..._],
              input: P.value,
              inst: J,
            }),
            {}
          );
      },
      error: K.error,
    }),
    D = new $({
      type: "pipe",
      in: new H({ type: "string", error: K.error }),
      out: J,
      error: K.error,
    });
  return new $({
    type: "pipe",
    in: D,
    out: new O({ type: "boolean", error: K.error }),
    error: K.error,
  });
}
function Um1(A, q, K, Y = {}) {
  let z = V7(Y),
    w = {
      ...V7(Y),
      check: "string_format",
      type: "string",
      format: q,
      fn: typeof K === "function" ? K : ($) => K.test($),
      ...z,
    };
  if (K instanceof RegExp) w.pattern = K;
  return new A(w);
}
var wm1;
var dm1 = E(() => {
  Qs6();
  DE6();
  A3();
  wm1 = { Any: null, Minute: -1, Second: 0, Millisecond: 3, Microsecond: 6 };
});
class cm1 {
  constructor(A) {
    ((this._def = A), (this.def = A));
  }
  implement(A) {
    if (typeof A !== "function")
      throw Error("implement() must be called with a function");
    let q = (...K) => {
      let Y = this._def.input
        ? wE6(this._def.input, K, void 0, { callee: q })
        : K;
      if (!Array.isArray(Y))
        throw Error("Invalid arguments schema: not an array or tuple schema.");
      let z = A(...Y);
      return this._def.output
        ? wE6(this._def.output, z, void 0, { callee: q })
        : z;
    };
    return q;
  }
  implementAsync(A) {
    if (typeof A !== "function")
      throw Error("implement() must be called with a function");
    let q = async (...K) => {
      let Y = this._def.input
        ? await _E6(this._def.input, K, void 0, { callee: q })
        : K;
      if (!Array.isArray(Y))
        throw Error("Invalid arguments schema: not an array or tuple schema.");
      let z = await A(...Y);
      return this._def.output
        ? _E6(this._def.output, z, void 0, { callee: q })
        : z;
    };
    return q;
  }
  input(...A) {
    let q = this.constructor;
    if (Array.isArray(A[0]))
      return new q({
        type: "function",
        input: new aA6({ type: "tuple", items: A[0], rest: A[1] }),
        output: this._def.output,
      });
    return new q({ type: "function", input: A[0], output: this._def.output });
  }
  output(A) {
    return new this.constructor({
      type: "function",
      input: this._def.input,
      output: A,
    });
  }
}
function lm1(A) {
  return new cm1({
    type: "function",
    input: Array.isArray(A?.input)
      ? Bm1(aA6, A?.input)
      : (A?.input ?? RE6(HE6, j$6(O$6))),
    output: A?.output ?? j$6(O$6),
  });
}
var h6A = E(() => {
  dm1();
  ms6();
  DE6();
  DE6();
});
class Gt6 {
  constructor(A) {
    ((this.counter = 0),
      (this.metadataRegistry = A?.metadata ?? Ku),
      (this.target = A?.target ?? "draft-2020-12"),
      (this.unrepresentable = A?.unrepresentable ?? "throw"),
      (this.override = A?.override ?? (() => {})),
      (this.io = A?.io ?? "output"),
      (this.seen = new Map()));
  }
  process(A, q = { path: [], schemaPath: [] }) {
    var K;
    let Y = A._zod.def,
      z = {
        guid: "uuid",
        url: "uri",
        datetime: "date-time",
        json_string: "json-string",
        regex: "",
      },
      w = this.seen.get(A);
    if (w) {
      if ((w.count++, q.schemaPath.includes(A))) w.cycle = q.path;
      return w.schema;
    }
    let _ = { schema: {}, count: 1, cycle: void 0, path: q.path };
    this.seen.set(A, _);
    let $ = A._zod.toJSONSchema?.();
    if ($) _.schema = $;
    else {
      let j = { ...q, schemaPath: [...q.schemaPath, A], path: q.path },
        J = A._zod.parent;
      if (J)
        ((_.ref = J), this.process(J, j), (this.seen.get(J).isParent = !0));
      else {
        let D = _.schema;
        switch (Y.type) {
          case "string": {
            let X = D;
            X.type = "string";
            let {
              minimum: M,
              maximum: P,
              format: W,
              patterns: G,
              contentEncoding: Z,
            } = A._zod.bag;
            if (typeof M === "number") X.minLength = M;
            if (typeof P === "number") X.maxLength = P;
            if (W) {
              if (((X.format = z[W] ?? W), X.format === "")) delete X.format;
            }
            if (Z) X.contentEncoding = Z;
            if (G && G.size > 0) {
              let f = [...G];
              if (f.length === 1) X.pattern = f[0].source;
              else if (f.length > 1)
                _.schema.allOf = [
                  ...f.map((N) => ({
                    ...(this.target === "draft-7" ? { type: "string" } : {}),
                    pattern: N.source,
                  })),
                ];
            }
            break;
          }
          case "number": {
            let X = D,
              {
                minimum: M,
                maximum: P,
                format: W,
                multipleOf: G,
                exclusiveMaximum: Z,
                exclusiveMinimum: f,
              } = A._zod.bag;
            if (typeof W === "string" && W.includes("int")) X.type = "integer";
            else X.type = "number";
            if (typeof f === "number") X.exclusiveMinimum = f;
            if (typeof M === "number") {
              if (((X.minimum = M), typeof f === "number"))
                if (f >= M) delete X.minimum;
                else delete X.exclusiveMinimum;
            }
            if (typeof Z === "number") X.exclusiveMaximum = Z;
            if (typeof P === "number") {
              if (((X.maximum = P), typeof Z === "number"))
                if (Z <= P) delete X.maximum;
                else delete X.exclusiveMaximum;
            }
            if (typeof G === "number") X.multipleOf = G;
            break;
          }
          case "boolean": {
            let X = D;
            X.type = "boolean";
            break;
          }
          case "bigint": {
            if (this.unrepresentable === "throw")
              throw Error("BigInt cannot be represented in JSON Schema");
            break;
          }
          case "symbol": {
            if (this.unrepresentable === "throw")
              throw Error("Symbols cannot be represented in JSON Schema");
            break;
          }
          case "null": {
            D.type = "null";
            break;
          }
          case "any":
            break;
          case "unknown":
            break;
          case "undefined":
          case "never": {
            D.not = {};
            break;
          }
          case "void": {
            if (this.unrepresentable === "throw")
              throw Error("Void cannot be represented in JSON Schema");
            break;
          }
          case "date": {
            if (this.unrepresentable === "throw")
              throw Error("Date cannot be represented in JSON Schema");
            break;
          }
          case "array": {
            let X = D,
              { minimum: M, maximum: P } = A._zod.bag;
            if (typeof M === "number") X.minItems = M;
            if (typeof P === "number") X.maxItems = P;
            ((X.type = "array"),
              (X.items = this.process(Y.element, {
                ...j,
                path: [...j.path, "items"],
              })));
            break;
          }
          case "object": {
            let X = D;
            ((X.type = "object"), (X.properties = {}));
            let M = Y.shape;
            for (let G in M)
              X.properties[G] = this.process(M[G], {
                ...j,
                path: [...j.path, "properties", G],
              });
            let P = new Set(Object.keys(M)),
              W = new Set(
                [...P].filter((G) => {
                  let Z = Y.shape[G]._zod;
                  if (this.io === "input") return Z.optin === void 0;
                  else return Z.optout === void 0;
                }),
              );
            if (W.size > 0) X.required = Array.from(W);
            if (Y.catchall?._zod.def.type === "never")
              X.additionalProperties = !1;
            else if (!Y.catchall) {
              if (this.io === "output") X.additionalProperties = !1;
            } else if (Y.catchall)
              X.additionalProperties = this.process(Y.catchall, {
                ...j,
                path: [...j.path, "additionalProperties"],
              });
            break;
          }
          case "union": {
            let X = D;
            X.anyOf = Y.options.map((M, P) =>
              this.process(M, { ...j, path: [...j.path, "anyOf", P] }),
            );
            break;
          }
          case "intersection": {
            let X = D,
              M = this.process(Y.left, { ...j, path: [...j.path, "allOf", 0] }),
              P = this.process(Y.right, {
                ...j,
                path: [...j.path, "allOf", 1],
              }),
              W = (Z) => "allOf" in Z && Object.keys(Z).length === 1,
              G = [...(W(M) ? M.allOf : [M]), ...(W(P) ? P.allOf : [P])];
            X.allOf = G;
            break;
          }
          case "tuple": {
            let X = D;
            X.type = "array";
            let M = Y.items.map((G, Z) =>
              this.process(G, { ...j, path: [...j.path, "prefixItems", Z] }),
            );
            if (this.target === "draft-2020-12") X.prefixItems = M;
            else X.items = M;
            if (Y.rest) {
              let G = this.process(Y.rest, {
                ...j,
                path: [...j.path, "items"],
              });
              if (this.target === "draft-2020-12") X.items = G;
              else X.additionalItems = G;
            }
            if (Y.rest)
              X.items = this.process(Y.rest, {
                ...j,
                path: [...j.path, "items"],
              });
            let { minimum: P, maximum: W } = A._zod.bag;
            if (typeof P === "number") X.minItems = P;
            if (typeof W === "number") X.maxItems = W;
            break;
          }
          case "record": {
            let X = D;
            ((X.type = "object"),
              (X.propertyNames = this.process(Y.keyType, {
                ...j,
                path: [...j.path, "propertyNames"],
              })),
              (X.additionalProperties = this.process(Y.valueType, {
                ...j,
                path: [...j.path, "additionalProperties"],
              })));
            break;
          }
          case "map": {
            if (this.unrepresentable === "throw")
              throw Error("Map cannot be represented in JSON Schema");
            break;
          }
          case "set": {
            if (this.unrepresentable === "throw")
              throw Error("Set cannot be represented in JSON Schema");
            break;
          }
          case "enum": {
            let X = D,
              M = ak6(Y.entries);
            if (M.every((P) => typeof P === "number")) X.type = "number";
            if (M.every((P) => typeof P === "string")) X.type = "string";
            X.enum = M;
            break;
          }
          case "literal": {
            let X = D,
              M = [];
            for (let P of Y.values)
              if (P === void 0) {
                if (this.unrepresentable === "throw")
                  throw Error(
                    "Literal `undefined` cannot be represented in JSON Schema",
                  );
              } else if (typeof P === "bigint")
                if (this.unrepresentable === "throw")
                  throw Error(
                    "BigInt literals cannot be represented in JSON Schema",
                  );
                else M.push(Number(P));
              else M.push(P);
            if (M.length === 0);
            else if (M.length === 1) {
              let P = M[0];
              ((X.type = P === null ? "null" : typeof P), (X.const = P));
            } else {
              if (M.every((P) => typeof P === "number")) X.type = "number";
              if (M.every((P) => typeof P === "string")) X.type = "string";
              if (M.every((P) => typeof P === "boolean")) X.type = "string";
              if (M.every((P) => P === null)) X.type = "null";
              X.enum = M;
            }
            break;
          }
          case "file": {
            let X = D,
              M = {
                type: "string",
                format: "binary",
                contentEncoding: "binary",
              },
              { minimum: P, maximum: W, mime: G } = A._zod.bag;
            if (P !== void 0) M.minLength = P;
            if (W !== void 0) M.maxLength = W;
            if (G)
              if (G.length === 1)
                ((M.contentMediaType = G[0]), Object.assign(X, M));
              else
                X.anyOf = G.map((Z) => {
                  return { ...M, contentMediaType: Z };
                });
            else Object.assign(X, M);
            break;
          }
          case "transform": {
            if (this.unrepresentable === "throw")
              throw Error("Transforms cannot be represented in JSON Schema");
            break;
          }
          case "nullable": {
            let X = this.process(Y.innerType, j);
            D.anyOf = [X, { type: "null" }];
            break;
          }
          case "nonoptional": {
            (this.process(Y.innerType, j), (_.ref = Y.innerType));
            break;
          }
          case "success": {
            let X = D;
            X.type = "boolean";
            break;
          }
          case "default": {
            (this.process(Y.innerType, j),
              (_.ref = Y.innerType),
              (D.default = JSON.parse(JSON.stringify(Y.defaultValue))));
            break;
          }
          case "prefault": {
            if (
              (this.process(Y.innerType, j),
              (_.ref = Y.innerType),
              this.io === "input")
            )
              D._prefault = JSON.parse(JSON.stringify(Y.defaultValue));
            break;
          }
          case "catch": {
            (this.process(Y.innerType, j), (_.ref = Y.innerType));
            let X;
            try {
              X = Y.catchValue(void 0);
            } catch {
              throw Error(
                "Dynamic catch values are not supported in JSON Schema",
              );
            }
            D.default = X;
            break;
          }
          case "nan": {
            if (this.unrepresentable === "throw")
              throw Error("NaN cannot be represented in JSON Schema");
            break;
          }
          case "template_literal": {
            let X = D,
              M = A._zod.pattern;
            if (!M) throw Error("Pattern not found in template literal");
            ((X.type = "string"), (X.pattern = M.source));
            break;
          }
          case "pipe": {
            let X =
              this.io === "input"
                ? Y.in._zod.def.type === "transform"
                  ? Y.out
                  : Y.in
                : Y.out;
            (this.process(X, j), (_.ref = X));
            break;
          }
          case "readonly": {
            (this.process(Y.innerType, j),
              (_.ref = Y.innerType),
              (D.readOnly = !0));
            break;
          }
          case "promise": {
            (this.process(Y.innerType, j), (_.ref = Y.innerType));
            break;
          }
          case "optional": {
            (this.process(Y.innerType, j), (_.ref = Y.innerType));
            break;
          }
          case "lazy": {
            let X = A._zod.innerType;
            (this.process(X, j), (_.ref = X));
            break;
          }
          case "custom": {
            if (this.unrepresentable === "throw")
              throw Error("Custom types cannot be represented in JSON Schema");
            break;
          }
          default:
        }
      }
    }
    let O = this.metadataRegistry.get(A);
    if (O) Object.assign(_.schema, O);
    if (this.io === "input" && aD(A))
      (delete _.schema.examples, delete _.schema.default);
    if (this.io === "input" && _.schema._prefault)
      (K = _.schema).default ?? (K.default = _.schema._prefault);
    return (delete _.schema._prefault, this.seen.get(A).schema);
  }
  emit(A, q) {
    let K = {
        cycles: q?.cycles ?? "ref",
        reused: q?.reused ?? "inline",
        external: q?.external ?? void 0,
      },
      Y = this.seen.get(A);
    if (!Y) throw Error("Unprocessed schema. This is a bug in Zod.");
    let z = (H) => {
        let j = this.target === "draft-2020-12" ? "$defs" : "definitions";
        if (K.external) {
          let M = K.external.registry.get(H[0])?.id;
          if (M) return { ref: K.external.uri(M) };
          let P = H[1].defId ?? H[1].schema.id ?? `schema${this.counter++}`;
          return (
            (H[1].defId = P),
            { defId: P, ref: `${K.external.uri("__shared")}#/${j}/${P}` }
          );
        }
        if (H[1] === Y) return { ref: "#" };
        let D = `${"#"}/${j}/`,
          X = H[1].schema.id ?? `__schema${this.counter++}`;
        return { defId: X, ref: D + X };
      },
      w = (H) => {
        if (H[1].schema.$ref) return;
        let j = H[1],
          { ref: J, defId: D } = z(H);
        if (((j.def = { ...j.schema }), D)) j.defId = D;
        let X = j.schema;
        for (let M in X) delete X[M];
        X.$ref = J;
      };
    for (let H of this.seen.entries()) {
      let j = H[1];
      if (A === H[0]) {
        w(H);
        continue;
      }
      if (K.external) {
        let D = K.external.registry.get(H[0])?.id;
        if (A !== H[0] && D) {
          w(H);
          continue;
        }
      }
      if (this.metadataRegistry.get(H[0])?.id) {
        w(H);
        continue;
      }
      if (j.cycle) {
        if (K.cycles === "throw")
          throw Error(`Cycle detected: #/${j.cycle?.join("/")}/<root>

Set the \`cycles\` parameter to \`"ref"\` to resolve cyclical schemas with defs.`);
        else if (K.cycles === "ref") w(H);
        continue;
      }
      if (j.count > 1) {
        if (K.reused === "ref") {
          w(H);
          continue;
        }
      }
    }
    let _ = (H, j) => {
      let J = this.seen.get(H),
        D = J.def ?? J.schema,
        X = { ...D };
      if (J.ref === null) return;
      let M = J.ref;
      if (((J.ref = null), M)) {
        _(M, j);
        let P = this.seen.get(M).schema;
        if (P.$ref && j.target === "draft-7")
          ((D.allOf = D.allOf ?? []), D.allOf.push(P));
        else (Object.assign(D, P), Object.assign(D, X));
      }
      if (!J.isParent)
        this.override({ zodSchema: H, jsonSchema: D, path: J.path ?? [] });
    };
    for (let H of [...this.seen.entries()].reverse())
      _(H[0], { target: this.target });
    let $ = {};
    if (this.target === "draft-2020-12")
      $.$schema = "https://json-schema.org/draft/2020-12/schema";
    else if (this.target === "draft-7")
      $.$schema = "http://json-schema.org/draft-07/schema#";
    else console.warn(`Invalid target: ${this.target}`);
    Object.assign($, Y.def);
    let O = K.external?.defs ?? {};
    for (let H of this.seen.entries()) {
      let j = H[1];
      if (j.def && j.defId) O[j.defId] = j.def;
    }
    if (!K.external && Object.keys(O).length > 0)
      if (this.target === "draft-2020-12") $.$defs = O;
      else $.definitions = O;
    try {
      return JSON.parse(JSON.stringify($));
    } catch (H) {
      throw Error("Error converting schema to JSON.");
    }
  }
}
function zQ(A, q) {
  if (A instanceof ME6) {
    let Y = new Gt6(q),
      z = {};
    for (let $ of A._idmap.entries()) {
      let [O, H] = $;
      Y.process(H);
    }
    let w = {},
      _ = { registry: A, uri: q?.uri || (($) => $), defs: z };
    for (let $ of A._idmap.entries()) {
      let [O, H] = $;
      w[O] = Y.emit(H, { ...q, external: _ });
    }
    if (Object.keys(z).length > 0) {
      let $ = Y.target === "draft-2020-12" ? "$defs" : "definitions";
      w.__shared = { [$]: z };
    }
    return { schemas: w };
  }
  let K = new Gt6(q);
  return (K.process(A), K.emit(A, q));
}
function aD(A, q) {
  let K = q ?? { seen: new Set() };
  if (K.seen.has(A)) return !1;
  K.seen.add(A);
  let z = A._zod.def;
  switch (z.type) {
    case "string":
    case "number":
    case "bigint":
    case "boolean":
    case "date":
    case "symbol":
    case "undefined":
    case "null":
    case "any":
    case "unknown":
    case "never":
    case "void":
    case "literal":
    case "enum":
    case "nan":
    case "file":
    case "template_literal":
      return !1;
    case "array":
      return aD(z.element, K);
    case "object": {
      for (let w in z.shape) if (aD(z.shape[w], K)) return !0;
      return !1;
    }
    case "union": {
      for (let w of z.options) if (aD(w, K)) return !0;
      return !1;
    }
    case "intersection":
      return aD(z.left, K) || aD(z.right, K);
    case "tuple": {
      for (let w of z.items) if (aD(w, K)) return !0;
      if (z.rest && aD(z.rest, K)) return !0;
      return !1;
    }
    case "record":
      return aD(z.keyType, K) || aD(z.valueType, K);
    case "map":
      return aD(z.keyType, K) || aD(z.valueType, K);
    case "set":
      return aD(z.valueType, K);
    case "promise":
    case "optional":
    case "nonoptional":
    case "nullable":
    case "readonly":
      return aD(z.innerType, K);
    case "lazy":
      return aD(z.getter(), K);
    case "default":
      return aD(z.innerType, K);
    case "prefault":
      return aD(z.innerType, K);
    case "custom":
      return !1;
    case "transform":
      return !0;
    case "pipe":
      return aD(z.in, K) || aD(z.out, K);
    case "success":
      return !1;
    case "catch":
      return !1;
    default:
  }
  throw Error(`Unknown schema type: ${z.type}`);
}
var I6A = E(() => {
  Km1();
  A3();
});
var x6A = {};
var b6A = () => {};
var Yu = {};
s1(Yu, {
  version: () => jb1,
  util: () => u7,
  treeifyError: () => Mx1,
  toJSONSchema: () => zQ,
  toDotPath: () => Ee8,
  safeParseAsync: () => $E6,
  safeParse: () => _$6,
  registry: () => os6,
  regexes: () => rA6,
  prettifyError: () => Px1,
  parseAsync: () => _E6,
  parse: () => wE6,
  locales: () => H$6,
  isValidJWT: () => de8,
  isValidBase64URL: () => Ue8,
  isValidBase64: () => Ib1,
  globalRegistry: () => Ku,
  globalConfig: () => nk6,
  function: () => lm1,
  formatError: () => zE6,
  flattenError: () => YE6,
  config: () => bJ,
  clone: () => Vv,
  _xid: () => $t6,
  _void: () => Rm1,
  _uuidv7: () => At6,
  _uuidv6: () => es6,
  _uuidv4: () => ts6,
  _uuid: () => ss6,
  _url: () => qt6,
  _uppercase: () => fE6,
  _unknown: () => j$6,
  _union: () => rcq,
  _undefined: () => km1,
  _ulid: () => _t6,
  _uint64: () => Vm1,
  _uint32: () => Wm1,
  _tuple: () => Bm1,
  _trim: () => EE6,
  _transform: () => Ylq,
  _toUpperCase: () => yE6,
  _toLowerCase: () => LE6,
  _templateLiteral: () => Dlq,
  _symbol: () => vm1,
  _success: () => Olq,
  _stringbool: () => Qm1,
  _stringFormat: () => Um1,
  _string: () => Ym1,
  _startsWith: () => NE6,
  _size: () => WE6,
  _set: () => ecq,
  _safeParseAsync: () => us6,
  _safeParse: () => bs6,
  _regex: () => GE6,
  _refine: () => pm1,
  _record: () => scq,
  _readonly: () => Jlq,
  _property: () => mm1,
  _promise: () => Mlq,
  _positive: () => Im1,
  _pipe: () => jlq,
  _parseAsync: () => xs6,
  _parse: () => Is6,
  _overwrite: () => YQ,
  _optional: () => zlq,
  _number: () => jm1,
  _nullable: () => wlq,
  _null: () => Em1,
  _normalize: () => kE6,
  _nonpositive: () => bm1,
  _nonoptional: () => $lq,
  _nonnegative: () => um1,
  _never: () => ym1,
  _negative: () => xm1,
  _nativeEnum: () => qlq,
  _nanoid: () => Yt6,
  _nan: () => hm1,
  _multipleOf: () => sA6,
  _minSize: () => tA6,
  _minLength: () => qr,
  _min: () => gT,
  _mime: () => vE6,
  _maxSize: () => J$6,
  _maxLength: () => D$6,
  _max: () => ZL,
  _map: () => tcq,
  _lte: () => ZL,
  _lt: () => qQ,
  _lowercase: () => ZE6,
  _literal: () => Klq,
  _length: () => X$6,
  _lazy: () => Xlq,
  _ksuid: () => Ot6,
  _jwt: () => Wt6,
  _isoTime: () => Om1,
  _isoDuration: () => Hm1,
  _isoDateTime: () => _m1,
  _isoDate: () => $m1,
  _ipv6: () => jt6,
  _ipv4: () => Ht6,
  _intersection: () => acq,
  _int64: () => Nm1,
  _int32: () => Pm1,
  _int: () => Dm1,
  _includes: () => TE6,
  _guid: () => PE6,
  _gte: () => gT,
  _gt: () => KQ,
  _float64: () => Mm1,
  _float32: () => Xm1,
  _file: () => gm1,
  _enum: () => Alq,
  _endsWith: () => VE6,
  _emoji: () => Kt6,
  _email: () => as6,
  _e164: () => Pt6,
  _discriminatedUnion: () => ocq,
  _default: () => _lq,
  _date: () => Cm1,
  _custom: () => Fm1,
  _cuid2: () => wt6,
  _cuid: () => zt6,
  _coercedString: () => zm1,
  _coercedNumber: () => Jm1,
  _coercedDate: () => Sm1,
  _coercedBoolean: () => Zm1,
  _coercedBigint: () => Tm1,
  _cidrv6: () => Dt6,
  _cidrv4: () => Jt6,
  _catch: () => Hlq,
  _boolean: () => Gm1,
  _bigint: () => fm1,
  _base64url: () => Mt6,
  _base64: () => Xt6,
  _array: () => RE6,
  _any: () => Lm1,
  TimePrecision: () => wm1,
  NEVER: () => rk6,
  JSONSchemaGenerator: () => Gt6,
  JSONSchema: () => x6A,
  Doc: () => Us6,
  $output: () => Am1,
  $input: () => qm1,
  $constructor: () => f8,
  $brand: () => Yx1,
  $ZodXID: () => Vb1,
  $ZodVoid: () => lb1,
  $ZodUnknown: () => O$6,
  $ZodUnion: () => ns6,
  $ZodUndefined: () => Qb1,
  $ZodUUID: () => Mb1,
  $ZodURL: () => Wb1,
  $ZodULID: () => Nb1,
  $ZodType: () => T3,
  $ZodTuple: () => aA6,
  $ZodTransform: () => jE6,
  $ZodTemplateLiteral: () => Ju1,
  $ZodSymbol: () => pb1,
  $ZodSuccess: () => $u1,
  $ZodStringFormat: () => Uw,
  $ZodString: () => oA6,
  $ZodSet: () => tb1,
  $ZodRegistry: () => ME6,
  $ZodRecord: () => ab1,
  $ZodRealError: () => w$6,
  $ZodReadonly: () => ju1,
  $ZodPromise: () => Du1,
  $ZodPrefault: () => wu1,
  $ZodPipe: () => JE6,
  $ZodOptional: () => Ku1,
  $ZodObject: () => nb1,
  $ZodNumberFormat: () => gb1,
  $ZodNumber: () => ls6,
  $ZodNullable: () => Yu1,
  $ZodNull: () => Ub1,
  $ZodNonOptional: () => _u1,
  $ZodNever: () => cb1,
  $ZodNanoID: () => Zb1,
  $ZodNaN: () => Hu1,
  $ZodMap: () => sb1,
  $ZodLiteral: () => Au1,
  $ZodLazy: () => Xu1,
  $ZodKSUID: () => vb1,
  $ZodJWT: () => mb1,
  $ZodIntersection: () => ob1,
  $ZodISOTime: () => Lb1,
  $ZodISODuration: () => yb1,
  $ZodISODateTime: () => kb1,
  $ZodISODate: () => Eb1,
  $ZodIPv6: () => Cb1,
  $ZodIPv4: () => Rb1,
  $ZodGUID: () => Xb1,
  $ZodFunction: () => cm1,
  $ZodFile: () => qu1,
  $ZodError: () => KE6,
  $ZodEnum: () => eb1,
  $ZodEmoji: () => Gb1,
  $ZodEmail: () => Pb1,
  $ZodE164: () => ub1,
  $ZodDiscriminatedUnion: () => rb1,
  $ZodDefault: () => zu1,
  $ZodDate: () => ib1,
  $ZodCustomStringFormat: () => Bb1,
  $ZodCustom: () => Mu1,
  $ZodCheckUpperCase: () => Yb1,
  $ZodCheckStringFormat: () => $$6,
  $ZodCheckStartsWith: () => wb1,
  $ZodCheckSizeEquals: () => sx1,
  $ZodCheckRegex: () => qb1,
  $ZodCheckProperty: () => $b1,
  $ZodCheckOverwrite: () => Hb1,
  $ZodCheckNumberFormat: () => nx1,
  $ZodCheckMultipleOf: () => ix1,
  $ZodCheckMinSize: () => ax1,
  $ZodCheckMinLength: () => ex1,
  $ZodCheckMimeType: () => Ob1,
  $ZodCheckMaxSize: () => ox1,
  $ZodCheckMaxLength: () => tx1,
  $ZodCheckLowerCase: () => Kb1,
  $ZodCheckLessThan: () => Fs6,
  $ZodCheckLengthEquals: () => Ab1,
  $ZodCheckIncludes: () => zb1,
  $ZodCheckGreaterThan: () => ps6,
  $ZodCheckEndsWith: () => _b1,
  $ZodCheckBigIntFormat: () => rx1,
  $ZodCheck: () => cO,
  $ZodCatch: () => Ou1,
  $ZodCUID2: () => Tb1,
  $ZodCUID: () => fb1,
  $ZodCIDRv6: () => hb1,
  $ZodCIDRv4: () => Sb1,
  $ZodBoolean: () => OE6,
  $ZodBigIntFormat: () => Fb1,
  $ZodBigInt: () => is6,
  $ZodBase64URL: () => bb1,
  $ZodBase64: () => xb1,
  $ZodAsyncError: () => ep,
  $ZodArray: () => HE6,
  $ZodAny: () => db1,
});
var hZ = E(() => {
  A3();
  gs6();
  rs6();
  b6A();
  K$6();
  ms6();
  Wx1();
  DE6();
  Qs6();
  Jb1();
  Km1();
  h6A();
  dm1();
  I6A();
});
var im1 = E(() => {
  hZ();
});
var M$6 = {};
s1(M$6, {
  time: () => om1,
  duration: () => am1,
  datetime: () => nm1,
  date: () => rm1,
  ZodISOTime: () => Tt6,
  ZodISODuration: () => Nt6,
  ZodISODateTime: () => Zt6,
  ZodISODate: () => ft6,
});
function nm1(A) {
  return _m1(Zt6, A);
}
function rm1(A) {
  return $m1(ft6, A);
}
function om1(A) {
  return Om1(Tt6, A);
}
function am1(A) {
  return Hm1(Nt6, A);
}
var Zt6, ft6, Tt6, Nt6;
var Vt6 = E(() => {
  hZ();
  vt6();
  Zt6 = f8("ZodISODateTime", (A, q) => {
    (kb1.init(A, q), L_.init(A, q));
  });
  ft6 = f8("ZodISODate", (A, q) => {
    (Eb1.init(A, q), L_.init(A, q));
  });
  Tt6 = f8("ZodISOTime", (A, q) => {
    (Lb1.init(A, q), L_.init(A, q));
  });
  Nt6 = f8("ZodISODuration", (A, q) => {
    (yb1.init(A, q), L_.init(A, q));
  });
});
var m6A = (A, q) => {
    (KE6.init(A, q),
      (A.name = "ZodError"),
      Object.defineProperties(A, {
        format: { value: (K) => zE6(A, K) },
        flatten: { value: (K) => YE6(A, K) },
        addIssue: { value: (K) => A.issues.push(K) },
        addIssues: { value: (K) => A.issues.push(...K) },
        isEmpty: {
          get() {
            return A.issues.length === 0;
          },
        },
      }));
  },
  Wlq,
  P$6;
var sm1 = E(() => {
  hZ();
  hZ();
  ((Wlq = f8("ZodError", m6A)), (P$6 = f8("ZodError", m6A, { Parent: Error })));
});
var tm1, em1, AB1, qB1;
var KB1 = E(() => {
  hZ();
  sm1();
  ((tm1 = Is6(P$6)), (em1 = xs6(P$6)), (AB1 = bs6(P$6)), (qB1 = us6(P$6)));
});
function c1(A) {
  return Ym1(SE6, A);
}
function Zlq(A) {
  return as6(wB1, A);
}
function flq(A) {
  return PE6(kt6, A);
}
function Tlq(A) {
  return ss6(wQ, A);
}
function Nlq(A) {
  return ts6(wQ, A);
}
function Vlq(A) {
  return es6(wQ, A);
}
function vlq(A) {
  return At6(wQ, A);
}
function $B1(A) {
  return qt6(_B1, A);
}
function klq(A) {
  return Kt6(OB1, A);
}
function Elq(A) {
  return Yt6(HB1, A);
}
function Llq(A) {
  return zt6(jB1, A);
}
function ylq(A) {
  return wt6(JB1, A);
}
function Rlq(A) {
  return _t6(DB1, A);
}
function Clq(A) {
  return $t6(XB1, A);
}
function Slq(A) {
  return Ot6(MB1, A);
}
function hlq(A) {
  return Ht6(PB1, A);
}
function Ilq(A) {
  return jt6(WB1, A);
}
function xlq(A) {
  return Jt6(GB1, A);
}
function blq(A) {
  return Dt6(ZB1, A);
}
function ulq(A) {
  return Xt6(fB1, A);
}
function mlq(A) {
  return Mt6(TB1, A);
}
function Blq(A) {
  return Pt6(NB1, A);
}
function glq(A) {
  return Wt6(VB1, A);
}
function Flq(A, q, K = {}) {
  return Um1(B6A, A, q, K);
}
function xY(A) {
  return jm1(hE6, A);
}
function YB1(A) {
  return Dm1(W$6, A);
}
function plq(A) {
  return Xm1(W$6, A);
}
function Qlq(A) {
  return Mm1(W$6, A);
}
function Ulq(A) {
  return Pm1(W$6, A);
}
function dlq(A) {
  return Wm1(W$6, A);
}
function B2(A) {
  return Gm1(IE6, A);
}
function clq(A) {
  return fm1(xE6, A);
}
function llq(A) {
  return Nm1(vB1, A);
}
function ilq(A) {
  return Vm1(vB1, A);
}
function nlq(A) {
  return vm1(g6A, A);
}
function rlq(A) {
  return km1(F6A, A);
}
function bE6(A) {
  return Em1(p6A, A);
}
function kB1() {
  return Lm1(Q6A);
}
function W$() {
  return j$6(U6A);
}
function yt6(A) {
  return ym1(d6A, A);
}
function olq(A) {
  return Rm1(c6A, A);
}
function alq(A) {
  return Cm1(Rt6, A);
}
function m7(A, q) {
  return RE6(l6A, A, q);
}
function slq(A) {
  let q = A._zod.def.shape;
  return Oq(Object.keys(q));
}
function n7(A, q) {
  let K = {
    type: "object",
    get shape() {
      return (u7.assignProp(this, "shape", { ...A }), this.shape);
    },
    ...u7.normalizeParams(q),
  };
  return new Ct6(K);
}
function tlq(A, q) {
  return new Ct6({
    type: "object",
    get shape() {
      return (u7.assignProp(this, "shape", { ...A }), this.shape);
    },
    catchall: yt6(),
    ...u7.normalizeParams(q),
  });
}
function uJ(A, q) {
  return new Ct6({
    type: "object",
    get shape() {
      return (u7.assignProp(this, "shape", { ...A }), this.shape);
    },
    catchall: W$(),
    ...u7.normalizeParams(q),
  });
}
function g2(A, q) {
  return new EB1({ type: "union", options: A, ...u7.normalizeParams(q) });
}
function St6(A, q, K) {
  return new i6A({
    type: "union",
    options: q,
    discriminator: A,
    ...u7.normalizeParams(K),
  });
}
function uE6(A, q) {
  return new n6A({ type: "intersection", left: A, right: q });
}
function elq(A, q, K) {
  let Y = q instanceof T3,
    z = Y ? K : q;
  return new r6A({
    type: "tuple",
    items: A,
    rest: Y ? q : null,
    ...u7.normalizeParams(z),
  });
}
function y_(A, q, K) {
  return new LB1({
    type: "record",
    keyType: A,
    valueType: q,
    ...u7.normalizeParams(K),
  });
}
function Aiq(A, q, K) {
  return new LB1({
    type: "record",
    keyType: g2([A, yt6()]),
    valueType: q,
    ...u7.normalizeParams(K),
  });
}
function qiq(A, q, K) {
  return new o6A({
    type: "map",
    keyType: A,
    valueType: q,
    ...u7.normalizeParams(K),
  });
}
function Kiq(A, q) {
  return new a6A({ type: "set", valueType: A, ...u7.normalizeParams(q) });
}
function IZ(A, q) {
  let K = Array.isArray(A) ? Object.fromEntries(A.map((Y) => [Y, Y])) : A;
  return new CE6({ type: "enum", entries: K, ...u7.normalizeParams(q) });
}
function Yiq(A, q) {
  return new CE6({ type: "enum", entries: A, ...u7.normalizeParams(q) });
}
function Oq(A, q) {
  return new s6A({
    type: "literal",
    values: Array.isArray(A) ? A : [A],
    ...u7.normalizeParams(q),
  });
}
function ziq(A) {
  return gm1(t6A, A);
}
function RB1(A) {
  return new yB1({ type: "transform", transform: A });
}
function G$(A) {
  return new CB1({ type: "optional", innerType: A });
}
function Et6(A) {
  return new e6A({ type: "nullable", innerType: A });
}
function wiq(A) {
  return G$(Et6(A));
}
function q1A(A, q) {
  return new A1A({
    type: "default",
    innerType: A,
    get defaultValue() {
      return typeof q === "function" ? q() : q;
    },
  });
}
function Y1A(A, q) {
  return new K1A({
    type: "prefault",
    innerType: A,
    get defaultValue() {
      return typeof q === "function" ? q() : q;
    },
  });
}
function z1A(A, q) {
  return new SB1({
    type: "nonoptional",
    innerType: A,
    ...u7.normalizeParams(q),
  });
}
function _iq(A) {
  return new w1A({ type: "success", innerType: A });
}
function $1A(A, q) {
  return new _1A({
    type: "catch",
    innerType: A,
    catchValue: typeof q === "function" ? q : () => q,
  });
}
function $iq(A) {
  return hm1(O1A, A);
}
function Lt6(A, q) {
  return new hB1({ type: "pipe", in: A, out: q });
}
function j1A(A) {
  return new H1A({ type: "readonly", innerType: A });
}
function Oiq(A, q) {
  return new J1A({
    type: "template_literal",
    parts: A,
    ...u7.normalizeParams(q),
  });
}
function X1A(A) {
  return new D1A({ type: "lazy", getter: A });
}
function Hiq(A) {
  return new M1A({ type: "promise", innerType: A });
}
function P1A(A, q) {
  let K = new cO({ check: "custom", ...u7.normalizeParams(q) });
  return ((K._zod.check = A), K);
}
function IB1(A, q) {
  return Fm1(ht6, A ?? (() => !0), q);
}
function W1A(A, q = {}) {
  return pm1(ht6, A, q);
}
function G1A(A, q) {
  let K = P1A((Y) => {
    return (
      (Y.addIssue = (z) => {
        if (typeof z === "string")
          Y.issues.push(u7.issue(z, Y.value, K._zod.def));
        else {
          let w = z;
          if (w.fatal) w.continue = !1;
          (w.code ?? (w.code = "custom"),
            w.input ?? (w.input = Y.value),
            w.inst ?? (w.inst = K),
            w.continue ?? (w.continue = !K._zod.def.abort),
            Y.issues.push(u7.issue(w)));
        }
      }),
      A(Y.value, Y)
    );
  }, q);
  return K;
}
function jiq(A, q = { error: `Input not instance of ${A.name}` }) {
  let K = new ht6({
    type: "custom",
    check: "custom",
    fn: (Y) => Y instanceof A,
    abort: !0,
    ...u7.normalizeParams(q),
  });
  return ((K._zod.bag.Class = A), K);
}
function Diq(A) {
  let q = X1A(() => {
    return g2([c1(A), xY(), B2(), bE6(), m7(q), y_(c1(), q)]);
  });
  return q;
}
function It6(A, q) {
  return Lt6(RB1(A), q);
}
var _9,
  zB1,
  SE6,
  L_,
  wB1,
  kt6,
  wQ,
  _B1,
  OB1,
  HB1,
  jB1,
  JB1,
  DB1,
  XB1,
  MB1,
  PB1,
  WB1,
  GB1,
  ZB1,
  fB1,
  TB1,
  NB1,
  VB1,
  B6A,
  hE6,
  W$6,
  IE6,
  xE6,
  vB1,
  g6A,
  F6A,
  p6A,
  Q6A,
  U6A,
  d6A,
  c6A,
  Rt6,
  l6A,
  Ct6,
  EB1,
  i6A,
  n6A,
  r6A,
  LB1,
  o6A,
  a6A,
  CE6,
  s6A,
  t6A,
  yB1,
  CB1,
  e6A,
  A1A,
  K1A,
  SB1,
  w1A,
  _1A,
  O1A,
  hB1,
  H1A,
  J1A,
  D1A,
  M1A,
  ht6,
  Jiq = (...A) =>
    Qm1({ Pipe: hB1, Boolean: IE6, String: SE6, Transform: yB1 }, ...A);
var vt6 = E(() => {
  hZ();
  hZ();
  im1();
  Vt6();
  KB1();
  ((_9 = f8("ZodType", (A, q) => {
    return (
      T3.init(A, q),
      (A.def = q),
      Object.defineProperty(A, "_def", { value: q }),
      (A.check = (...K) => {
        return A.clone({
          ...q,
          checks: [
            ...(q.checks ?? []),
            ...K.map((Y) =>
              typeof Y === "function"
                ? { _zod: { check: Y, def: { check: "custom" }, onattach: [] } }
                : Y,
            ),
          ],
        });
      }),
      (A.clone = (K, Y) => Vv(A, K, Y)),
      (A.brand = () => A),
      (A.register = (K, Y) => {
        return (K.add(A, Y), A);
      }),
      (A.parse = (K, Y) => tm1(A, K, Y, { callee: A.parse })),
      (A.safeParse = (K, Y) => AB1(A, K, Y)),
      (A.parseAsync = async (K, Y) => em1(A, K, Y, { callee: A.parseAsync })),
      (A.safeParseAsync = async (K, Y) => qB1(A, K, Y)),
      (A.spa = A.safeParseAsync),
      (A.refine = (K, Y) => A.check(W1A(K, Y))),
      (A.superRefine = (K) => A.check(G1A(K))),
      (A.overwrite = (K) => A.check(YQ(K))),
      (A.optional = () => G$(A)),
      (A.nullable = () => Et6(A)),
      (A.nullish = () => G$(Et6(A))),
      (A.nonoptional = (K) => z1A(A, K)),
      (A.array = () => m7(A)),
      (A.or = (K) => g2([A, K])),
      (A.and = (K) => uE6(A, K)),
      (A.transform = (K) => Lt6(A, RB1(K))),
      (A.default = (K) => q1A(A, K)),
      (A.prefault = (K) => Y1A(A, K)),
      (A.catch = (K) => $1A(A, K)),
      (A.pipe = (K) => Lt6(A, K)),
      (A.readonly = () => j1A(A)),
      (A.describe = (K) => {
        let Y = A.clone();
        return (Ku.add(Y, { description: K }), Y);
      }),
      Object.defineProperty(A, "description", {
        get() {
          return Ku.get(A)?.description;
        },
        configurable: !0,
      }),
      (A.meta = (...K) => {
        if (K.length === 0) return Ku.get(A);
        let Y = A.clone();
        return (Ku.add(Y, K[0]), Y);
      }),
      (A.isOptional = () => A.safeParse(void 0).success),
      (A.isNullable = () => A.safeParse(null).success),
      A
    );
  })),
    (zB1 = f8("_ZodString", (A, q) => {
      (oA6.init(A, q), _9.init(A, q));
      let K = A._zod.bag;
      ((A.format = K.format ?? null),
        (A.minLength = K.minimum ?? null),
        (A.maxLength = K.maximum ?? null),
        (A.regex = (...Y) => A.check(GE6(...Y))),
        (A.includes = (...Y) => A.check(TE6(...Y))),
        (A.startsWith = (...Y) => A.check(NE6(...Y))),
        (A.endsWith = (...Y) => A.check(VE6(...Y))),
        (A.min = (...Y) => A.check(qr(...Y))),
        (A.max = (...Y) => A.check(D$6(...Y))),
        (A.length = (...Y) => A.check(X$6(...Y))),
        (A.nonempty = (...Y) => A.check(qr(1, ...Y))),
        (A.lowercase = (Y) => A.check(ZE6(Y))),
        (A.uppercase = (Y) => A.check(fE6(Y))),
        (A.trim = () => A.check(EE6())),
        (A.normalize = (...Y) => A.check(kE6(...Y))),
        (A.toLowerCase = () => A.check(LE6())),
        (A.toUpperCase = () => A.check(yE6())));
    })),
    (SE6 = f8("ZodString", (A, q) => {
      (oA6.init(A, q),
        zB1.init(A, q),
        (A.email = (K) => A.check(as6(wB1, K))),
        (A.url = (K) => A.check(qt6(_B1, K))),
        (A.jwt = (K) => A.check(Wt6(VB1, K))),
        (A.emoji = (K) => A.check(Kt6(OB1, K))),
        (A.guid = (K) => A.check(PE6(kt6, K))),
        (A.uuid = (K) => A.check(ss6(wQ, K))),
        (A.uuidv4 = (K) => A.check(ts6(wQ, K))),
        (A.uuidv6 = (K) => A.check(es6(wQ, K))),
        (A.uuidv7 = (K) => A.check(At6(wQ, K))),
        (A.nanoid = (K) => A.check(Yt6(HB1, K))),
        (A.guid = (K) => A.check(PE6(kt6, K))),
        (A.cuid = (K) => A.check(zt6(jB1, K))),
        (A.cuid2 = (K) => A.check(wt6(JB1, K))),
        (A.ulid = (K) => A.check(_t6(DB1, K))),
        (A.base64 = (K) => A.check(Xt6(fB1, K))),
        (A.base64url = (K) => A.check(Mt6(TB1, K))),
        (A.xid = (K) => A.check($t6(XB1, K))),
        (A.ksuid = (K) => A.check(Ot6(MB1, K))),
        (A.ipv4 = (K) => A.check(Ht6(PB1, K))),
        (A.ipv6 = (K) => A.check(jt6(WB1, K))),
        (A.cidrv4 = (K) => A.check(Jt6(GB1, K))),
        (A.cidrv6 = (K) => A.check(Dt6(ZB1, K))),
        (A.e164 = (K) => A.check(Pt6(NB1, K))),
        (A.datetime = (K) => A.check(nm1(K))),
        (A.date = (K) => A.check(rm1(K))),
        (A.time = (K) => A.check(om1(K))),
        (A.duration = (K) => A.check(am1(K))));
    })));
  ((L_ = f8("ZodStringFormat", (A, q) => {
    (Uw.init(A, q), zB1.init(A, q));
  })),
    (wB1 = f8("ZodEmail", (A, q) => {
      (Pb1.init(A, q), L_.init(A, q));
    })));
  kt6 = f8("ZodGUID", (A, q) => {
    (Xb1.init(A, q), L_.init(A, q));
  });
  wQ = f8("ZodUUID", (A, q) => {
    (Mb1.init(A, q), L_.init(A, q));
  });
  _B1 = f8("ZodURL", (A, q) => {
    (Wb1.init(A, q), L_.init(A, q));
  });
  OB1 = f8("ZodEmoji", (A, q) => {
    (Gb1.init(A, q), L_.init(A, q));
  });
  HB1 = f8("ZodNanoID", (A, q) => {
    (Zb1.init(A, q), L_.init(A, q));
  });
  jB1 = f8("ZodCUID", (A, q) => {
    (fb1.init(A, q), L_.init(A, q));
  });
  JB1 = f8("ZodCUID2", (A, q) => {
    (Tb1.init(A, q), L_.init(A, q));
  });
  DB1 = f8("ZodULID", (A, q) => {
    (Nb1.init(A, q), L_.init(A, q));
  });
  XB1 = f8("ZodXID", (A, q) => {
    (Vb1.init(A, q), L_.init(A, q));
  });
  MB1 = f8("ZodKSUID", (A, q) => {
    (vb1.init(A, q), L_.init(A, q));
  });
  PB1 = f8("ZodIPv4", (A, q) => {
    (Rb1.init(A, q), L_.init(A, q));
  });
  WB1 = f8("ZodIPv6", (A, q) => {
    (Cb1.init(A, q), L_.init(A, q));
  });
  GB1 = f8("ZodCIDRv4", (A, q) => {
    (Sb1.init(A, q), L_.init(A, q));
  });
  ZB1 = f8("ZodCIDRv6", (A, q) => {
    (hb1.init(A, q), L_.init(A, q));
  });
  fB1 = f8("ZodBase64", (A, q) => {
    (xb1.init(A, q), L_.init(A, q));
  });
  TB1 = f8("ZodBase64URL", (A, q) => {
    (bb1.init(A, q), L_.init(A, q));
  });
  NB1 = f8("ZodE164", (A, q) => {
    (ub1.init(A, q), L_.init(A, q));
  });
  VB1 = f8("ZodJWT", (A, q) => {
    (mb1.init(A, q), L_.init(A, q));
  });
  B6A = f8("ZodCustomStringFormat", (A, q) => {
    (Bb1.init(A, q), L_.init(A, q));
  });
  hE6 = f8("ZodNumber", (A, q) => {
    (ls6.init(A, q),
      _9.init(A, q),
      (A.gt = (Y, z) => A.check(KQ(Y, z))),
      (A.gte = (Y, z) => A.check(gT(Y, z))),
      (A.min = (Y, z) => A.check(gT(Y, z))),
      (A.lt = (Y, z) => A.check(qQ(Y, z))),
      (A.lte = (Y, z) => A.check(ZL(Y, z))),
      (A.max = (Y, z) => A.check(ZL(Y, z))),
      (A.int = (Y) => A.check(YB1(Y))),
      (A.safe = (Y) => A.check(YB1(Y))),
      (A.positive = (Y) => A.check(KQ(0, Y))),
      (A.nonnegative = (Y) => A.check(gT(0, Y))),
      (A.negative = (Y) => A.check(qQ(0, Y))),
      (A.nonpositive = (Y) => A.check(ZL(0, Y))),
      (A.multipleOf = (Y, z) => A.check(sA6(Y, z))),
      (A.step = (Y, z) => A.check(sA6(Y, z))),
      (A.finite = () => A));
    let K = A._zod.bag;
    ((A.minValue =
      Math.max(
        K.minimum ?? Number.NEGATIVE_INFINITY,
        K.exclusiveMinimum ?? Number.NEGATIVE_INFINITY,
      ) ?? null),
      (A.maxValue =
        Math.min(
          K.maximum ?? Number.POSITIVE_INFINITY,
          K.exclusiveMaximum ?? Number.POSITIVE_INFINITY,
        ) ?? null),
      (A.isInt =
        (K.format ?? "").includes("int") ||
        Number.isSafeInteger(K.multipleOf ?? 0.5)),
      (A.isFinite = !0),
      (A.format = K.format ?? null));
  });
  W$6 = f8("ZodNumberFormat", (A, q) => {
    (gb1.init(A, q), hE6.init(A, q));
  });
  IE6 = f8("ZodBoolean", (A, q) => {
    (OE6.init(A, q), _9.init(A, q));
  });
  xE6 = f8("ZodBigInt", (A, q) => {
    (is6.init(A, q),
      _9.init(A, q),
      (A.gte = (Y, z) => A.check(gT(Y, z))),
      (A.min = (Y, z) => A.check(gT(Y, z))),
      (A.gt = (Y, z) => A.check(KQ(Y, z))),
      (A.gte = (Y, z) => A.check(gT(Y, z))),
      (A.min = (Y, z) => A.check(gT(Y, z))),
      (A.lt = (Y, z) => A.check(qQ(Y, z))),
      (A.lte = (Y, z) => A.check(ZL(Y, z))),
      (A.max = (Y, z) => A.check(ZL(Y, z))),
      (A.positive = (Y) => A.check(KQ(BigInt(0), Y))),
      (A.negative = (Y) => A.check(qQ(BigInt(0), Y))),
      (A.nonpositive = (Y) => A.check(ZL(BigInt(0), Y))),
      (A.nonnegative = (Y) => A.check(gT(BigInt(0), Y))),
      (A.multipleOf = (Y, z) => A.check(sA6(Y, z))));
    let K = A._zod.bag;
    ((A.minValue = K.minimum ?? null),
      (A.maxValue = K.maximum ?? null),
      (A.format = K.format ?? null));
  });
  vB1 = f8("ZodBigIntFormat", (A, q) => {
    (Fb1.init(A, q), xE6.init(A, q));
  });
  g6A = f8("ZodSymbol", (A, q) => {
    (pb1.init(A, q), _9.init(A, q));
  });
  F6A = f8("ZodUndefined", (A, q) => {
    (Qb1.init(A, q), _9.init(A, q));
  });
  p6A = f8("ZodNull", (A, q) => {
    (Ub1.init(A, q), _9.init(A, q));
  });
  Q6A = f8("ZodAny", (A, q) => {
    (db1.init(A, q), _9.init(A, q));
  });
  U6A = f8("ZodUnknown", (A, q) => {
    (O$6.init(A, q), _9.init(A, q));
  });
  d6A = f8("ZodNever", (A, q) => {
    (cb1.init(A, q), _9.init(A, q));
  });
  c6A = f8("ZodVoid", (A, q) => {
    (lb1.init(A, q), _9.init(A, q));
  });
  Rt6 = f8("ZodDate", (A, q) => {
    (ib1.init(A, q),
      _9.init(A, q),
      (A.min = (Y, z) => A.check(gT(Y, z))),
      (A.max = (Y, z) => A.check(ZL(Y, z))));
    let K = A._zod.bag;
    ((A.minDate = K.minimum ? new Date(K.minimum) : null),
      (A.maxDate = K.maximum ? new Date(K.maximum) : null));
  });
  l6A = f8("ZodArray", (A, q) => {
    (HE6.init(A, q),
      _9.init(A, q),
      (A.element = q.element),
      (A.min = (K, Y) => A.check(qr(K, Y))),
      (A.nonempty = (K) => A.check(qr(1, K))),
      (A.max = (K, Y) => A.check(D$6(K, Y))),
      (A.length = (K, Y) => A.check(X$6(K, Y))),
      (A.unwrap = () => A.element));
  });
  Ct6 = f8("ZodObject", (A, q) => {
    (nb1.init(A, q),
      _9.init(A, q),
      u7.defineLazy(A, "shape", () => q.shape),
      (A.keyof = () => IZ(Object.keys(A._zod.def.shape))),
      (A.catchall = (K) => A.clone({ ...A._zod.def, catchall: K })),
      (A.passthrough = () => A.clone({ ...A._zod.def, catchall: W$() })),
      (A.loose = () => A.clone({ ...A._zod.def, catchall: W$() })),
      (A.strict = () => A.clone({ ...A._zod.def, catchall: yt6() })),
      (A.strip = () => A.clone({ ...A._zod.def, catchall: void 0 })),
      (A.extend = (K) => {
        return u7.extend(A, K);
      }),
      (A.merge = (K) => u7.merge(A, K)),
      (A.pick = (K) => u7.pick(A, K)),
      (A.omit = (K) => u7.omit(A, K)),
      (A.partial = (...K) => u7.partial(CB1, A, K[0])),
      (A.required = (...K) => u7.required(SB1, A, K[0])));
  });
  EB1 = f8("ZodUnion", (A, q) => {
    (ns6.init(A, q), _9.init(A, q), (A.options = q.options));
  });
  i6A = f8("ZodDiscriminatedUnion", (A, q) => {
    (EB1.init(A, q), rb1.init(A, q));
  });
  n6A = f8("ZodIntersection", (A, q) => {
    (ob1.init(A, q), _9.init(A, q));
  });
  r6A = f8("ZodTuple", (A, q) => {
    (aA6.init(A, q),
      _9.init(A, q),
      (A.rest = (K) => A.clone({ ...A._zod.def, rest: K })));
  });
  LB1 = f8("ZodRecord", (A, q) => {
    (ab1.init(A, q),
      _9.init(A, q),
      (A.keyType = q.keyType),
      (A.valueType = q.valueType));
  });
  o6A = f8("ZodMap", (A, q) => {
    (sb1.init(A, q),
      _9.init(A, q),
      (A.keyType = q.keyType),
      (A.valueType = q.valueType));
  });
  a6A = f8("ZodSet", (A, q) => {
    (tb1.init(A, q),
      _9.init(A, q),
      (A.min = (...K) => A.check(tA6(...K))),
      (A.nonempty = (K) => A.check(tA6(1, K))),
      (A.max = (...K) => A.check(J$6(...K))),
      (A.size = (...K) => A.check(WE6(...K))));
  });
  CE6 = f8("ZodEnum", (A, q) => {
    (eb1.init(A, q),
      _9.init(A, q),
      (A.enum = q.entries),
      (A.options = Object.values(q.entries)));
    let K = new Set(Object.keys(q.entries));
    ((A.extract = (Y, z) => {
      let w = {};
      for (let _ of Y)
        if (K.has(_)) w[_] = q.entries[_];
        else throw Error(`Key ${_} not found in enum`);
      return new CE6({
        ...q,
        checks: [],
        ...u7.normalizeParams(z),
        entries: w,
      });
    }),
      (A.exclude = (Y, z) => {
        let w = { ...q.entries };
        for (let _ of Y)
          if (K.has(_)) delete w[_];
          else throw Error(`Key ${_} not found in enum`);
        return new CE6({
          ...q,
          checks: [],
          ...u7.normalizeParams(z),
          entries: w,
        });
      }));
  });
  s6A = f8("ZodLiteral", (A, q) => {
    (Au1.init(A, q),
      _9.init(A, q),
      (A.values = new Set(q.values)),
      Object.defineProperty(A, "value", {
        get() {
          if (q.values.length > 1)
            throw Error(
              "This schema contains multiple valid literal values. Use `.values` instead.",
            );
          return q.values[0];
        },
      }));
  });
  t6A = f8("ZodFile", (A, q) => {
    (qu1.init(A, q),
      _9.init(A, q),
      (A.min = (K, Y) => A.check(tA6(K, Y))),
      (A.max = (K, Y) => A.check(J$6(K, Y))),
      (A.mime = (K, Y) => A.check(vE6(Array.isArray(K) ? K : [K], Y))));
  });
  yB1 = f8("ZodTransform", (A, q) => {
    (jE6.init(A, q),
      _9.init(A, q),
      (A._zod.parse = (K, Y) => {
        K.addIssue = (w) => {
          if (typeof w === "string") K.issues.push(u7.issue(w, K.value, q));
          else {
            let _ = w;
            if (_.fatal) _.continue = !1;
            (_.code ?? (_.code = "custom"),
              _.input ?? (_.input = K.value),
              _.inst ?? (_.inst = A),
              _.continue ?? (_.continue = !0),
              K.issues.push(u7.issue(_)));
          }
        };
        let z = q.transform(K.value, K);
        if (z instanceof Promise)
          return z.then((w) => {
            return ((K.value = w), K);
          });
        return ((K.value = z), K);
      }));
  });
  CB1 = f8("ZodOptional", (A, q) => {
    (Ku1.init(A, q), _9.init(A, q), (A.unwrap = () => A._zod.def.innerType));
  });
  e6A = f8("ZodNullable", (A, q) => {
    (Yu1.init(A, q), _9.init(A, q), (A.unwrap = () => A._zod.def.innerType));
  });
  A1A = f8("ZodDefault", (A, q) => {
    (zu1.init(A, q),
      _9.init(A, q),
      (A.unwrap = () => A._zod.def.innerType),
      (A.removeDefault = A.unwrap));
  });
  K1A = f8("ZodPrefault", (A, q) => {
    (wu1.init(A, q), _9.init(A, q), (A.unwrap = () => A._zod.def.innerType));
  });
  SB1 = f8("ZodNonOptional", (A, q) => {
    (_u1.init(A, q), _9.init(A, q), (A.unwrap = () => A._zod.def.innerType));
  });
  w1A = f8("ZodSuccess", (A, q) => {
    ($u1.init(A, q), _9.init(A, q), (A.unwrap = () => A._zod.def.innerType));
  });
  _1A = f8("ZodCatch", (A, q) => {
    (Ou1.init(A, q),
      _9.init(A, q),
      (A.unwrap = () => A._zod.def.innerType),
      (A.removeCatch = A.unwrap));
  });
  O1A = f8("ZodNaN", (A, q) => {
    (Hu1.init(A, q), _9.init(A, q));
  });
  hB1 = f8("ZodPipe", (A, q) => {
    (JE6.init(A, q), _9.init(A, q), (A.in = q.in), (A.out = q.out));
  });
  H1A = f8("ZodReadonly", (A, q) => {
    (ju1.init(A, q), _9.init(A, q));
  });
  J1A = f8("ZodTemplateLiteral", (A, q) => {
    (Ju1.init(A, q), _9.init(A, q));
  });
  D1A = f8("ZodLazy", (A, q) => {
    (Xu1.init(A, q), _9.init(A, q), (A.unwrap = () => A._zod.def.getter()));
  });
  M1A = f8("ZodPromise", (A, q) => {
    (Du1.init(A, q), _9.init(A, q), (A.unwrap = () => A._zod.def.innerType));
  });
  ht6 = f8("ZodCustom", (A, q) => {
    (Mu1.init(A, q), _9.init(A, q));
  });
});
function Xiq(A) {
  bJ({ customError: A });
}
function Miq() {
  return bJ().customError;
}
var xB1;
var Z1A = E(() => {
  hZ();
  xB1 = {
    invalid_type: "invalid_type",
    too_big: "too_big",
    too_small: "too_small",
    invalid_format: "invalid_format",
    not_multiple_of: "not_multiple_of",
    unrecognized_keys: "unrecognized_keys",
    invalid_union: "invalid_union",
    invalid_key: "invalid_key",
    invalid_element: "invalid_element",
    invalid_value: "invalid_value",
    custom: "custom",
  };
});
var mE6 = {};
s1(mE6, {
  string: () => Piq,
  number: () => Wiq,
  date: () => fiq,
  boolean: () => Giq,
  bigint: () => Ziq,
});
function Piq(A) {
  return zm1(SE6, A);
}
function Wiq(A) {
  return Jm1(hE6, A);
}
function Giq(A) {
  return Zm1(IE6, A);
}
function Ziq(A) {
  return Tm1(xE6, A);
}
function fiq(A) {
  return Sm1(Rt6, A);
}
var f1A = E(() => {
  hZ();
  vt6();
});
var x = {};
s1(x, {
  xid: () => Clq,
  void: () => olq,
  uuidv7: () => vlq,
  uuidv6: () => Vlq,
  uuidv4: () => Nlq,
  uuid: () => Tlq,
  url: () => $B1,
  uppercase: () => fE6,
  unknown: () => W$,
  union: () => g2,
  undefined: () => rlq,
  ulid: () => Rlq,
  uint64: () => ilq,
  uint32: () => dlq,
  tuple: () => elq,
  trim: () => EE6,
  treeifyError: () => Mx1,
  transform: () => RB1,
  toUpperCase: () => yE6,
  toLowerCase: () => LE6,
  toJSONSchema: () => zQ,
  templateLiteral: () => Oiq,
  symbol: () => nlq,
  superRefine: () => G1A,
  success: () => _iq,
  stringbool: () => Jiq,
  stringFormat: () => Flq,
  string: () => c1,
  strictObject: () => tlq,
  startsWith: () => NE6,
  size: () => WE6,
  setErrorMap: () => Xiq,
  set: () => Kiq,
  safeParseAsync: () => qB1,
  safeParse: () => AB1,
  registry: () => os6,
  regexes: () => rA6,
  regex: () => GE6,
  refine: () => W1A,
  record: () => y_,
  readonly: () => j1A,
  property: () => mm1,
  promise: () => Hiq,
  prettifyError: () => Px1,
  preprocess: () => It6,
  prefault: () => Y1A,
  positive: () => Im1,
  pipe: () => Lt6,
  partialRecord: () => Aiq,
  parseAsync: () => em1,
  parse: () => tm1,
  overwrite: () => YQ,
  optional: () => G$,
  object: () => n7,
  number: () => xY,
  nullish: () => wiq,
  nullable: () => Et6,
  null: () => bE6,
  normalize: () => kE6,
  nonpositive: () => bm1,
  nonoptional: () => z1A,
  nonnegative: () => um1,
  never: () => yt6,
  negative: () => xm1,
  nativeEnum: () => Yiq,
  nanoid: () => Elq,
  nan: () => $iq,
  multipleOf: () => sA6,
  minSize: () => tA6,
  minLength: () => qr,
  mime: () => vE6,
  maxSize: () => J$6,
  maxLength: () => D$6,
  map: () => qiq,
  lte: () => ZL,
  lt: () => qQ,
  lowercase: () => ZE6,
  looseObject: () => uJ,
  locales: () => H$6,
  literal: () => Oq,
  length: () => X$6,
  lazy: () => X1A,
  ksuid: () => Slq,
  keyof: () => slq,
  jwt: () => glq,
  json: () => Diq,
  iso: () => M$6,
  ipv6: () => Ilq,
  ipv4: () => hlq,
  intersection: () => uE6,
  int64: () => llq,
  int32: () => Ulq,
  int: () => YB1,
  instanceof: () => jiq,
  includes: () => TE6,
  guid: () => flq,
  gte: () => gT,
  gt: () => KQ,
  globalRegistry: () => Ku,
  getErrorMap: () => Miq,
  function: () => lm1,
  formatError: () => zE6,
  float64: () => Qlq,
  float32: () => plq,
  flattenError: () => YE6,
  file: () => ziq,
  enum: () => IZ,
  endsWith: () => VE6,
  emoji: () => klq,
  email: () => Zlq,
  e164: () => Blq,
  discriminatedUnion: () => St6,
  date: () => alq,
  custom: () => IB1,
  cuid2: () => ylq,
  cuid: () => Llq,
  core: () => Yu,
  config: () => bJ,
  coerce: () => mE6,
  clone: () => Vv,
  cidrv6: () => blq,
  cidrv4: () => xlq,
  check: () => P1A,
  catch: () => $1A,
  boolean: () => B2,
  bigint: () => clq,
  base64url: () => mlq,
  base64: () => ulq,
  array: () => m7,
  any: () => kB1,
  _default: () => q1A,
  _ZodString: () => zB1,
  ZodXID: () => XB1,
  ZodVoid: () => c6A,
  ZodUnknown: () => U6A,
  ZodUnion: () => EB1,
  ZodUndefined: () => F6A,
  ZodUUID: () => wQ,
  ZodURL: () => _B1,
  ZodULID: () => DB1,
  ZodType: () => _9,
  ZodTuple: () => r6A,
  ZodTransform: () => yB1,
  ZodTemplateLiteral: () => J1A,
  ZodSymbol: () => g6A,
  ZodSuccess: () => w1A,
  ZodStringFormat: () => L_,
  ZodString: () => SE6,
  ZodSet: () => a6A,
  ZodRecord: () => LB1,
  ZodRealError: () => P$6,
  ZodReadonly: () => H1A,
  ZodPromise: () => M1A,
  ZodPrefault: () => K1A,
  ZodPipe: () => hB1,
  ZodOptional: () => CB1,
  ZodObject: () => Ct6,
  ZodNumberFormat: () => W$6,
  ZodNumber: () => hE6,
  ZodNullable: () => e6A,
  ZodNull: () => p6A,
  ZodNonOptional: () => SB1,
  ZodNever: () => d6A,
  ZodNanoID: () => HB1,
  ZodNaN: () => O1A,
  ZodMap: () => o6A,
  ZodLiteral: () => s6A,
  ZodLazy: () => D1A,
  ZodKSUID: () => MB1,
  ZodJWT: () => VB1,
  ZodIssueCode: () => xB1,
  ZodIntersection: () => n6A,
  ZodISOTime: () => Tt6,
  ZodISODuration: () => Nt6,
  ZodISODateTime: () => Zt6,
  ZodISODate: () => ft6,
  ZodIPv6: () => WB1,
  ZodIPv4: () => PB1,
  ZodGUID: () => kt6,
  ZodFile: () => t6A,
  ZodError: () => Wlq,
  ZodEnum: () => CE6,
  ZodEmoji: () => OB1,
  ZodEmail: () => wB1,
  ZodE164: () => NB1,
  ZodDiscriminatedUnion: () => i6A,
  ZodDefault: () => A1A,
  ZodDate: () => Rt6,
  ZodCustomStringFormat: () => B6A,
  ZodCustom: () => ht6,
  ZodCatch: () => _1A,
  ZodCUID2: () => JB1,
  ZodCUID: () => jB1,
  ZodCIDRv6: () => ZB1,
  ZodCIDRv4: () => GB1,
  ZodBoolean: () => IE6,
  ZodBigIntFormat: () => vB1,
  ZodBigInt: () => xE6,
  ZodBase64URL: () => TB1,
  ZodBase64: () => fB1,
  ZodArray: () => l6A,
  ZodAny: () => Q6A,
  TimePrecision: () => wm1,
  NEVER: () => rk6,
  $output: () => Am1,
  $input: () => qm1,
  $brand: () => Yx1,
});
var bB1 = E(() => {
  hZ();
  hZ();
  Nu1();
  hZ();
  rs6();
  Vt6();
  Vt6();
  f1A();
  vt6();
  im1();
  sm1();
  KB1();
  Z1A();
  bJ(XE6());
});
var T1A;
var uB1 = E(() => {
  bB1();
  bB1();
  T1A = x;
});
var I4;
var K4 = E(() => {
  uB1();
  uB1();
  I4 = T1A;
});
var Kr = "2025-11-25",
  bt6,
  Yr = "io.modelcontextprotocol/related-task",
  ut6 = "2.0",
  f0,
  V1A,
  v1A,
  Npz,
  Tiq,
  Niq,
  mB1,
  kv,
  BE6,
  k1A = (A) => BE6.safeParse(A).success,
  T0,
  fL,
  TL,
  N0,
  mt6,
  E1A,
  gE6 = (A) => E1A.safeParse(A).success,
  L1A,
  y1A = (A) => L1A.safeParse(A).success,
  BB1,
  eA6 = (A) => BB1.safeParse(A).success,
  sq,
  gB1,
  R1A = (A) => gB1.safeParse(A).success,
  ES,
  Vpz,
  _Q,
  Viq,
  Bt6,
  viq,
  FE6,
  G$6,
  C1A,
  kiq,
  Eiq,
  Liq,
  yiq,
  Riq,
  Ciq,
  FB1,
  Siq,
  pB1,
  gt6,
  S1A = (A) => gt6.safeParse(A).success,
  Ft6,
  hiq,
  Iiq,
  pt6,
  xiq,
  pE6,
  QE6,
  biq,
  UE6,
  $Q,
  uiq,
  dE6,
  Qt6,
  Ut6,
  dt6,
  vpz,
  ct6,
  lt6,
  it6,
  h1A,
  I1A,
  x1A,
  QB1,
  b1A,
  cE6,
  Z$6,
  u1A,
  miq,
  Biq,
  A76,
  giq,
  UB1,
  dB1,
  Fiq,
  piq,
  lE6,
  iE6,
  Qiq,
  Uiq,
  diq,
  ciq,
  liq,
  iiq,
  niq,
  riq,
  oiq,
  nE6,
  aiq,
  siq,
  cB1,
  lB1,
  iB1,
  tiq,
  eiq,
  Anq,
  nB1,
  qnq,
  rB1,
  rE6,
  Knq,
  Ynq,
  m1A,
  oE6,
  aE6,
  zu,
  kpz,
  znq,
  q76,
  sE6,
  B1A,
  tE6,
  wnq,
  oB1,
  _nq,
  $nq,
  Onq,
  Hnq,
  jnq,
  Jnq,
  Dnq,
  xt6,
  Xnq,
  Mnq,
  aB1,
  K76,
  eE6,
  Pnq,
  Wnq,
  Gnq,
  Znq,
  fnq,
  Tnq,
  Nnq,
  Vnq,
  vnq,
  knq,
  Enq,
  Lnq,
  ynq,
  Rnq,
  Cnq,
  OQ,
  Snq,
  AL6,
  zr,
  hnq,
  Inq,
  xnq,
  bnq,
  sB1,
  unq,
  tB1,
  eB1,
  mnq,
  Epz,
  Lpz,
  ypz,
  Rpz,
  Cpz,
  Spz,
  Hq,
  g1A;
var sD = E(() => {
  K4();
  ((bt6 = [Kr, "2025-06-18", "2025-03-26", "2024-11-05", "2024-10-07"]),
    (f0 = IB1(
      (A) => A !== null && (typeof A === "object" || typeof A === "function"),
    )),
    (V1A = g2([c1(), xY().int()])),
    (v1A = c1()),
    (Npz = uJ({
      ttl: g2([xY(), bE6()]).optional(),
      pollInterval: xY().optional(),
    })),
    (Tiq = n7({ ttl: xY().optional() })),
    (Niq = n7({ taskId: c1() })),
    (mB1 = uJ({ progressToken: V1A.optional(), [Yr]: Niq.optional() })),
    (kv = n7({ _meta: mB1.optional() })),
    (BE6 = kv.extend({ task: Tiq.optional() })),
    (T0 = n7({ method: c1(), params: kv.loose().optional() })),
    (fL = n7({ _meta: mB1.optional() })),
    (TL = n7({ method: c1(), params: fL.loose().optional() })),
    (N0 = uJ({ _meta: mB1.optional() })),
    (mt6 = g2([c1(), xY().int()])),
    (E1A = n7({ jsonrpc: Oq(ut6), id: mt6, ...T0.shape }).strict()),
    (L1A = n7({ jsonrpc: Oq(ut6), ...TL.shape }).strict()),
    (BB1 = n7({ jsonrpc: Oq(ut6), id: mt6, result: N0 }).strict()));
  (function (A) {
    ((A[(A.ConnectionClosed = -32000)] = "ConnectionClosed"),
      (A[(A.RequestTimeout = -32001)] = "RequestTimeout"),
      (A[(A.ParseError = -32700)] = "ParseError"),
      (A[(A.InvalidRequest = -32600)] = "InvalidRequest"),
      (A[(A.MethodNotFound = -32601)] = "MethodNotFound"),
      (A[(A.InvalidParams = -32602)] = "InvalidParams"),
      (A[(A.InternalError = -32603)] = "InternalError"),
      (A[(A.UrlElicitationRequired = -32042)] = "UrlElicitationRequired"));
  })(sq || (sq = {}));
  ((gB1 = n7({
    jsonrpc: Oq(ut6),
    id: mt6.optional(),
    error: n7({ code: xY().int(), message: c1(), data: W$().optional() }),
  }).strict()),
    (ES = g2([E1A, L1A, BB1, gB1])),
    (Vpz = g2([BB1, gB1])),
    (_Q = N0.strict()),
    (Viq = fL.extend({ requestId: mt6.optional(), reason: c1().optional() })),
    (Bt6 = TL.extend({ method: Oq("notifications/cancelled"), params: Viq })),
    (viq = n7({
      src: c1(),
      mimeType: c1().optional(),
      sizes: m7(c1()).optional(),
      theme: IZ(["light", "dark"]).optional(),
    })),
    (FE6 = n7({ icons: m7(viq).optional() })),
    (G$6 = n7({ name: c1(), title: c1().optional() })),
    (C1A = G$6.extend({
      ...G$6.shape,
      ...FE6.shape,
      version: c1(),
      websiteUrl: c1().optional(),
      description: c1().optional(),
    })),
    (kiq = uE6(n7({ applyDefaults: B2().optional() }), y_(c1(), W$()))),
    (Eiq = It6(
      (A) => {
        if (A && typeof A === "object" && !Array.isArray(A)) {
          if (Object.keys(A).length === 0) return { form: {} };
        }
        return A;
      },
      uE6(
        n7({ form: kiq.optional(), url: f0.optional() }),
        y_(c1(), W$()).optional(),
      ),
    )),
    (Liq = uJ({
      list: f0.optional(),
      cancel: f0.optional(),
      requests: uJ({
        sampling: uJ({ createMessage: f0.optional() }).optional(),
        elicitation: uJ({ create: f0.optional() }).optional(),
      }).optional(),
    })),
    (yiq = uJ({
      list: f0.optional(),
      cancel: f0.optional(),
      requests: uJ({
        tools: uJ({ call: f0.optional() }).optional(),
      }).optional(),
    })),
    (Riq = n7({
      experimental: y_(c1(), f0).optional(),
      sampling: n7({ context: f0.optional(), tools: f0.optional() }).optional(),
      elicitation: Eiq.optional(),
      roots: n7({ listChanged: B2().optional() }).optional(),
      tasks: Liq.optional(),
    })),
    (Ciq = kv.extend({
      protocolVersion: c1(),
      capabilities: Riq,
      clientInfo: C1A,
    })),
    (FB1 = T0.extend({ method: Oq("initialize"), params: Ciq })),
    (Siq = n7({
      experimental: y_(c1(), f0).optional(),
      logging: f0.optional(),
      completions: f0.optional(),
      prompts: n7({ listChanged: B2().optional() }).optional(),
      resources: n7({
        subscribe: B2().optional(),
        listChanged: B2().optional(),
      }).optional(),
      tools: n7({ listChanged: B2().optional() }).optional(),
      tasks: yiq.optional(),
    })),
    (pB1 = N0.extend({
      protocolVersion: c1(),
      capabilities: Siq,
      serverInfo: C1A,
      instructions: c1().optional(),
    })),
    (gt6 = TL.extend({
      method: Oq("notifications/initialized"),
      params: fL.optional(),
    })),
    (Ft6 = T0.extend({ method: Oq("ping"), params: kv.optional() })),
    (hiq = n7({ progress: xY(), total: G$(xY()), message: G$(c1()) })),
    (Iiq = n7({ ...fL.shape, ...hiq.shape, progressToken: V1A })),
    (pt6 = TL.extend({ method: Oq("notifications/progress"), params: Iiq })),
    (xiq = kv.extend({ cursor: v1A.optional() })),
    (pE6 = T0.extend({ params: xiq.optional() })),
    (QE6 = N0.extend({ nextCursor: v1A.optional() })),
    (biq = IZ([
      "working",
      "input_required",
      "completed",
      "failed",
      "cancelled",
    ])),
    (UE6 = n7({
      taskId: c1(),
      status: biq,
      ttl: g2([xY(), bE6()]),
      createdAt: c1(),
      lastUpdatedAt: c1(),
      pollInterval: G$(xY()),
      statusMessage: G$(c1()),
    })),
    ($Q = N0.extend({ task: UE6 })),
    (uiq = fL.merge(UE6)),
    (dE6 = TL.extend({
      method: Oq("notifications/tasks/status"),
      params: uiq,
    })),
    (Qt6 = T0.extend({
      method: Oq("tasks/get"),
      params: kv.extend({ taskId: c1() }),
    })),
    (Ut6 = N0.merge(UE6)),
    (dt6 = T0.extend({
      method: Oq("tasks/result"),
      params: kv.extend({ taskId: c1() }),
    })),
    (vpz = N0.loose()),
    (ct6 = pE6.extend({ method: Oq("tasks/list") })),
    (lt6 = QE6.extend({ tasks: m7(UE6) })),
    (it6 = T0.extend({
      method: Oq("tasks/cancel"),
      params: kv.extend({ taskId: c1() }),
    })),
    (h1A = N0.merge(UE6)),
    (I1A = n7({
      uri: c1(),
      mimeType: G$(c1()),
      _meta: y_(c1(), W$()).optional(),
    })),
    (x1A = I1A.extend({ text: c1() })),
    (QB1 = c1().refine(
      (A) => {
        try {
          return (atob(A), !0);
        } catch {
          return !1;
        }
      },
      { message: "Invalid Base64 string" },
    )),
    (b1A = I1A.extend({ blob: QB1 })),
    (cE6 = IZ(["user", "assistant"])),
    (Z$6 = n7({
      audience: m7(cE6).optional(),
      priority: xY().min(0).max(1).optional(),
      lastModified: M$6.datetime({ offset: !0 }).optional(),
    })),
    (u1A = n7({
      ...G$6.shape,
      ...FE6.shape,
      uri: c1(),
      description: G$(c1()),
      mimeType: G$(c1()),
      annotations: Z$6.optional(),
      _meta: G$(uJ({})),
    })),
    (miq = n7({
      ...G$6.shape,
      ...FE6.shape,
      uriTemplate: c1(),
      description: G$(c1()),
      mimeType: G$(c1()),
      annotations: Z$6.optional(),
      _meta: G$(uJ({})),
    })),
    (Biq = pE6.extend({ method: Oq("resources/list") })),
    (A76 = QE6.extend({ resources: m7(u1A) })),
    (giq = pE6.extend({ method: Oq("resources/templates/list") })),
    (UB1 = QE6.extend({ resourceTemplates: m7(miq) })),
    (dB1 = kv.extend({ uri: c1() })),
    (Fiq = dB1),
    (piq = T0.extend({ method: Oq("resources/read"), params: Fiq })),
    (lE6 = N0.extend({ contents: m7(g2([x1A, b1A])) })),
    (iE6 = TL.extend({
      method: Oq("notifications/resources/list_changed"),
      params: fL.optional(),
    })),
    (Qiq = dB1),
    (Uiq = T0.extend({ method: Oq("resources/subscribe"), params: Qiq })),
    (diq = dB1),
    (ciq = T0.extend({ method: Oq("resources/unsubscribe"), params: diq })),
    (liq = fL.extend({ uri: c1() })),
    (iiq = TL.extend({
      method: Oq("notifications/resources/updated"),
      params: liq,
    })),
    (niq = n7({ name: c1(), description: G$(c1()), required: G$(B2()) })),
    (riq = n7({
      ...G$6.shape,
      ...FE6.shape,
      description: G$(c1()),
      arguments: G$(m7(niq)),
      _meta: G$(uJ({})),
    })),
    (oiq = pE6.extend({ method: Oq("prompts/list") })),
    (nE6 = QE6.extend({ prompts: m7(riq) })),
    (aiq = kv.extend({ name: c1(), arguments: y_(c1(), c1()).optional() })),
    (siq = T0.extend({ method: Oq("prompts/get"), params: aiq })),
    (cB1 = n7({
      type: Oq("text"),
      text: c1(),
      annotations: Z$6.optional(),
      _meta: y_(c1(), W$()).optional(),
    })),
    (lB1 = n7({
      type: Oq("image"),
      data: QB1,
      mimeType: c1(),
      annotations: Z$6.optional(),
      _meta: y_(c1(), W$()).optional(),
    })),
    (iB1 = n7({
      type: Oq("audio"),
      data: QB1,
      mimeType: c1(),
      annotations: Z$6.optional(),
      _meta: y_(c1(), W$()).optional(),
    })),
    (tiq = n7({
      type: Oq("tool_use"),
      name: c1(),
      id: c1(),
      input: y_(c1(), W$()),
      _meta: y_(c1(), W$()).optional(),
    })),
    (eiq = n7({
      type: Oq("resource"),
      resource: g2([x1A, b1A]),
      annotations: Z$6.optional(),
      _meta: y_(c1(), W$()).optional(),
    })),
    (Anq = u1A.extend({ type: Oq("resource_link") })),
    (nB1 = g2([cB1, lB1, iB1, Anq, eiq])),
    (qnq = n7({ role: cE6, content: nB1 })),
    (rB1 = N0.extend({ description: c1().optional(), messages: m7(qnq) })),
    (rE6 = TL.extend({
      method: Oq("notifications/prompts/list_changed"),
      params: fL.optional(),
    })),
    (Knq = n7({
      title: c1().optional(),
      readOnlyHint: B2().optional(),
      destructiveHint: B2().optional(),
      idempotentHint: B2().optional(),
      openWorldHint: B2().optional(),
    })),
    (Ynq = n7({
      taskSupport: IZ(["required", "optional", "forbidden"]).optional(),
    })),
    (m1A = n7({
      ...G$6.shape,
      ...FE6.shape,
      description: c1().optional(),
      inputSchema: n7({
        type: Oq("object"),
        properties: y_(c1(), f0).optional(),
        required: m7(c1()).optional(),
      }).catchall(W$()),
      outputSchema: n7({
        type: Oq("object"),
        properties: y_(c1(), f0).optional(),
        required: m7(c1()).optional(),
      })
        .catchall(W$())
        .optional(),
      annotations: Knq.optional(),
      execution: Ynq.optional(),
      _meta: y_(c1(), W$()).optional(),
    })),
    (oE6 = pE6.extend({ method: Oq("tools/list") })),
    (aE6 = QE6.extend({ tools: m7(m1A) })),
    (zu = N0.extend({
      content: m7(nB1).default([]),
      structuredContent: y_(c1(), W$()).optional(),
      isError: B2().optional(),
    })),
    (kpz = zu.or(N0.extend({ toolResult: W$() }))),
    (znq = BE6.extend({ name: c1(), arguments: y_(c1(), W$()).optional() })),
    (q76 = T0.extend({ method: Oq("tools/call"), params: znq })),
    (sE6 = TL.extend({
      method: Oq("notifications/tools/list_changed"),
      params: fL.optional(),
    })),
    (B1A = n7({
      autoRefresh: B2().default(!0),
      debounceMs: xY().int().nonnegative().default(300),
    })),
    (tE6 = IZ([
      "debug",
      "info",
      "notice",
      "warning",
      "error",
      "critical",
      "alert",
      "emergency",
    ])),
    (wnq = kv.extend({ level: tE6 })),
    (oB1 = T0.extend({ method: Oq("logging/setLevel"), params: wnq })),
    (_nq = fL.extend({ level: tE6, logger: c1().optional(), data: W$() })),
    ($nq = TL.extend({ method: Oq("notifications/message"), params: _nq })),
    (Onq = n7({ name: c1().optional() })),
    (Hnq = n7({
      hints: m7(Onq).optional(),
      costPriority: xY().min(0).max(1).optional(),
      speedPriority: xY().min(0).max(1).optional(),
      intelligencePriority: xY().min(0).max(1).optional(),
    })),
    (jnq = n7({ mode: IZ(["auto", "required", "none"]).optional() })),
    (Jnq = n7({
      type: Oq("tool_result"),
      toolUseId: c1().describe(
        "The unique identifier for the corresponding tool call.",
      ),
      content: m7(nB1).default([]),
      structuredContent: n7({}).loose().optional(),
      isError: B2().optional(),
      _meta: y_(c1(), W$()).optional(),
    })),
    (Dnq = St6("type", [cB1, lB1, iB1])),
    (xt6 = St6("type", [cB1, lB1, iB1, tiq, Jnq])),
    (Xnq = n7({
      role: cE6,
      content: g2([xt6, m7(xt6)]),
      _meta: y_(c1(), W$()).optional(),
    })),
    (Mnq = BE6.extend({
      messages: m7(Xnq),
      modelPreferences: Hnq.optional(),
      systemPrompt: c1().optional(),
      includeContext: IZ(["none", "thisServer", "allServers"]).optional(),
      temperature: xY().optional(),
      maxTokens: xY().int(),
      stopSequences: m7(c1()).optional(),
      metadata: f0.optional(),
      tools: m7(m1A).optional(),
      toolChoice: jnq.optional(),
    })),
    (aB1 = T0.extend({ method: Oq("sampling/createMessage"), params: Mnq })),
    (K76 = N0.extend({
      model: c1(),
      stopReason: G$(IZ(["endTurn", "stopSequence", "maxTokens"]).or(c1())),
      role: cE6,
      content: Dnq,
    })),
    (eE6 = N0.extend({
      model: c1(),
      stopReason: G$(
        IZ(["endTurn", "stopSequence", "maxTokens", "toolUse"]).or(c1()),
      ),
      role: cE6,
      content: g2([xt6, m7(xt6)]),
    })),
    (Pnq = n7({
      type: Oq("boolean"),
      title: c1().optional(),
      description: c1().optional(),
      default: B2().optional(),
    })),
    (Wnq = n7({
      type: Oq("string"),
      title: c1().optional(),
      description: c1().optional(),
      minLength: xY().optional(),
      maxLength: xY().optional(),
      format: IZ(["email", "uri", "date", "date-time"]).optional(),
      default: c1().optional(),
    })),
    (Gnq = n7({
      type: IZ(["number", "integer"]),
      title: c1().optional(),
      description: c1().optional(),
      minimum: xY().optional(),
      maximum: xY().optional(),
      default: xY().optional(),
    })),
    (Znq = n7({
      type: Oq("string"),
      title: c1().optional(),
      description: c1().optional(),
      enum: m7(c1()),
      default: c1().optional(),
    })),
    (fnq = n7({
      type: Oq("string"),
      title: c1().optional(),
      description: c1().optional(),
      oneOf: m7(n7({ const: c1(), title: c1() })),
      default: c1().optional(),
    })),
    (Tnq = n7({
      type: Oq("string"),
      title: c1().optional(),
      description: c1().optional(),
      enum: m7(c1()),
      enumNames: m7(c1()).optional(),
      default: c1().optional(),
    })),
    (Nnq = g2([Znq, fnq])),
    (Vnq = n7({
      type: Oq("array"),
      title: c1().optional(),
      description: c1().optional(),
      minItems: xY().optional(),
      maxItems: xY().optional(),
      items: n7({ type: Oq("string"), enum: m7(c1()) }),
      default: m7(c1()).optional(),
    })),
    (vnq = n7({
      type: Oq("array"),
      title: c1().optional(),
      description: c1().optional(),
      minItems: xY().optional(),
      maxItems: xY().optional(),
      items: n7({ anyOf: m7(n7({ const: c1(), title: c1() })) }),
      default: m7(c1()).optional(),
    })),
    (knq = g2([Vnq, vnq])),
    (Enq = g2([Tnq, Nnq, knq])),
    (Lnq = g2([Enq, Pnq, Wnq, Gnq])),
    (ynq = BE6.extend({
      mode: Oq("form").optional(),
      message: c1(),
      requestedSchema: n7({
        type: Oq("object"),
        properties: y_(c1(), Lnq),
        required: m7(c1()).optional(),
      }),
    })),
    (Rnq = BE6.extend({
      mode: Oq("url"),
      message: c1(),
      elicitationId: c1(),
      url: c1().url(),
    })),
    (Cnq = g2([ynq, Rnq])),
    (OQ = T0.extend({ method: Oq("elicitation/create"), params: Cnq })),
    (Snq = fL.extend({ elicitationId: c1() })),
    (AL6 = TL.extend({
      method: Oq("notifications/elicitation/complete"),
      params: Snq,
    })),
    (zr = N0.extend({
      action: IZ(["accept", "decline", "cancel"]),
      content: It6(
        (A) => (A === null ? void 0 : A),
        y_(c1(), g2([c1(), xY(), B2(), m7(c1())])).optional(),
      ),
    })),
    (hnq = n7({ type: Oq("ref/resource"), uri: c1() })),
    (Inq = n7({ type: Oq("ref/prompt"), name: c1() })),
    (xnq = kv.extend({
      ref: g2([Inq, hnq]),
      argument: n7({ name: c1(), value: c1() }),
      context: n7({ arguments: y_(c1(), c1()).optional() }).optional(),
    })),
    (bnq = T0.extend({ method: Oq("completion/complete"), params: xnq })),
    (sB1 = N0.extend({
      completion: uJ({
        values: m7(c1()).max(100),
        total: G$(xY().int()),
        hasMore: G$(B2()),
      }),
    })),
    (unq = n7({
      uri: c1().startsWith("file://"),
      name: c1().optional(),
      _meta: y_(c1(), W$()).optional(),
    })),
    (tB1 = T0.extend({ method: Oq("roots/list"), params: kv.optional() })),
    (eB1 = N0.extend({ roots: m7(unq) })),
    (mnq = TL.extend({
      method: Oq("notifications/roots/list_changed"),
      params: fL.optional(),
    })),
    (Epz = g2([
      Ft6,
      FB1,
      bnq,
      oB1,
      siq,
      oiq,
      Biq,
      giq,
      piq,
      Uiq,
      ciq,
      q76,
      oE6,
      Qt6,
      dt6,
      ct6,
      it6,
    ])),
    (Lpz = g2([Bt6, pt6, gt6, mnq, dE6])),
    (ypz = g2([_Q, K76, eE6, zr, eB1, Ut6, lt6, $Q])),
    (Rpz = g2([Ft6, aB1, OQ, tB1, Qt6, dt6, ct6, it6])),
    (Cpz = g2([Bt6, pt6, $nq, iiq, iE6, sE6, rE6, dE6, AL6])),
    (Spz = g2([_Q, pB1, sB1, rB1, nE6, A76, UB1, lE6, zu, aE6, Ut6, lt6, $Q])));
  Hq = class Hq extends Error {
    constructor(A, q, K) {
      super(`MCP error ${A}: ${q}`);
      ((this.code = A), (this.data = K), (this.name = "McpError"));
    }
    static fromError(A, q, K) {
      if (A === sq.UrlElicitationRequired && K) {
        let Y = K;
        if (Y.elicitations) return new g1A(Y.elicitations, q);
      }
      return new Hq(A, q, K);
    }
  };
  g1A = class g1A extends Hq {
    constructor(A, q = `URL elicitation${A.length > 1 ? "s" : ""} required`) {
      super(sq.UrlElicitationRequired, q, { elicitations: A });
    }
    get elicitations() {
      return this.data?.elicitations ?? [];
    }
  };
});
class qL6 {
  append(A) {
    this._buffer = this._buffer ? Buffer.concat([this._buffer, A]) : A;
  }
  readMessage() {
    if (!this._buffer) return null;
    let A = this._buffer.indexOf(`
`);
    if (A === -1) return null;
    let q = this._buffer.toString("utf8", 0, A).replace(/\r$/, "");
    return ((this._buffer = this._buffer.subarray(A + 1)), Bnq(q));
  }
  clear() {
    this._buffer = void 0;
  }
}
function Bnq(A) {
  return ES.parse(JSON.parse(A));
}
function nt6(A) {
  return (
    JSON.stringify(A) +
    `
`
  );
}
var Ag1 = E(() => {
  sD();
});
import F1A from "node:process";
class KL6 {
  constructor(A = F1A.stdin, q = F1A.stdout) {
    ((this._stdin = A),
      (this._stdout = q),
      (this._readBuffer = new qL6()),
      (this._started = !1),
      (this._ondata = (K) => {
        (this._readBuffer.append(K), this.processReadBuffer());
      }),
      (this._onerror = (K) => {
        this.onerror?.(K);
      }));
  }
  async start() {
    if (this._started)
      throw Error(
        "StdioServerTransport already started! If using Server class, note that connect() calls start() automatically.",
      );
    ((this._started = !0),
      this._stdin.on("data", this._ondata),
      this._stdin.on("error", this._onerror));
  }
  processReadBuffer() {
    while (!0)
      try {
        let A = this._readBuffer.readMessage();
        if (A === null) break;
        this.onmessage?.(A);
      } catch (A) {
        this.onerror?.(A);
      }
  }
  async close() {
    if (
      (this._stdin.off("data", this._ondata),
      this._stdin.off("error", this._onerror),
      this._stdin.listenerCount("data") === 0)
    )
      this._stdin.pause();
    (this._readBuffer.clear(), this.onclose?.());
  }
  send(A) {
    return new Promise((q) => {
      let K = nt6(A);
      if (this._stdout.write(K)) q();
      else this._stdout.once("drain", q);
    });
  }
}
var qg1 = E(() => {
  Ag1();
});
