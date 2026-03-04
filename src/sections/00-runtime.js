#!/usr/bin/env node
// (c) Anthropic PBC. All rights reserved. Use is subject to the Legal Agreements outlined here: https://code.claude.com/docs/en/legal-and-compliance.

// Version: 2.1.66

// Want to see the unminified source? We're hiring!
// https://job-boards.greenhouse.io/anthropic/jobs/4816199008
import { createRequire as amq } from "node:module";
var dmq = Object.create;
var {
    getPrototypeOf: cmq,
    defineProperty: jk6,
    getOwnPropertyNames: wo8,
    getOwnPropertyDescriptor: lmq,
  } = Object,
  _o8 = Object.prototype.hasOwnProperty;
function $o8(A) {
  return this[A];
}
var imq,
  nmq,
  Y6 = (A, q, K) => {
    var Y = A != null && typeof A === "object";
    if (Y) {
      var z = q ? (imq ??= new WeakMap()) : (nmq ??= new WeakMap()),
        w = z.get(A);
      if (w) return w;
    }
    K = A != null ? dmq(cmq(A)) : {};
    let _ =
      q || !A || !A.__esModule
        ? jk6(K, "default", { value: A, enumerable: !0 })
        : K;
    for (let $ of wo8(A))
      if (!_o8.call(_, $)) jk6(_, $, { get: $o8.bind(A, $), enumerable: !0 });
    if (Y) z.set(A, _);
    return _;
  },
  oX = (A) => {
    var q = (zo8 ??= new WeakMap()).get(A),
      K;
    if (q) return q;
    if (
      ((q = jk6({}, "__esModule", { value: !0 })),
      (A && typeof A === "object") || typeof A === "function")
    ) {
      for (var Y of wo8(A))
        if (!_o8.call(q, Y))
          jk6(q, Y, {
            get: $o8.bind(A, Y),
            enumerable: !(K = lmq(A, Y)) || K.enumerable,
          });
    }
    return (zo8.set(A, q), q);
  },
  zo8,
  C = (A, q) => () => (q || A((q = { exports: {} }).exports, q), q.exports);
var rmq = (A) => A;
function omq(A, q) {
  this[A] = rmq.bind(null, q);
}
var s1 = (A, q) => {
  for (var K in q)
    jk6(A, K, {
      get: q[K],
      enumerable: !0,
      configurable: !0,
      set: omq.bind(q, K),
    });
};
var E = (A, q) => () => (A && (q = A((A = 0))), q);
var require = amq(import.meta.url),
  smq = Symbol.dispose || Symbol.for("Symbol.dispose"),
  tmq = Symbol.asyncDispose || Symbol.for("Symbol.asyncDispose"),
  hY = (A, q, K) => {
    if (q != null) {
      if (typeof q !== "object" && typeof q !== "function")
        throw TypeError(
          'Object expected to be assigned to "using" declaration',
        );
      var Y;
      if (K) Y = q[tmq];
      if (Y === void 0) Y = q[smq];
      if (typeof Y !== "function") throw TypeError("Object not disposable");
      A.push([K, Y, q]);
    } else if (K) A.push([K]);
    return q;
  },
  IY = (A, q, K) => {
    var Y =
        typeof SuppressedError === "function"
          ? SuppressedError
          : function (_, $, O, H) {
              return (
                (H = Error(O)),
                (H.name = "SuppressedError"),
                (H.error = _),
                (H.suppressed = $),
                H
              );
            },
      z = (_) =>
        (q = K
          ? new Y(_, q, "An error was suppressed during disposal")
          : ((K = !0), _)),
      w = (_) => {
        while ((_ = A.pop()))
          try {
            var $ = _[1] && _[1].call(_[2]);
            if (_[0]) return Promise.resolve($).then(w, (O) => (z(O), w()));
          } catch (O) {
            z(O);
          }
        if (K) throw q;
      };
    return w();
  };
