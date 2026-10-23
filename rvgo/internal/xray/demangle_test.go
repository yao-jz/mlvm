package xray

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDemangleRust(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"_ZN4core3fmt5write17h3b2c2d1e0f1a2b3cE", "core::fmt::write"},
		{"_ZN3std2io5Write9write_all17h0123456789abcdefE", "std::io::Write::write_all"},
		{"_ZN90_$LT$candle_core..cpu_backend..MatMul$u20$as$u20$candle_core..cpu_backend..utils..Map2$GT$1f17h2bd6a2c6c8a3e0b1E",
			"<candle_core::cpu_backend::MatMul as candle_core::cpu_backend::utils::Map2>::f"},
		{"_ZN5alloc3vec16Vec$LT$T$C$A$GT$7reserve17h0000000000000000E", "alloc::vec::Vec<T,A>::reserve"},
		{"_ZN4gemm4gemm4gemm28_$u7b$$u7b$closure$u7d$$u7d$17haaaaaaaaaaaaaaaaE", "gemm::gemm::gemm::{{closure}}"},
		{"_ZN3foo3bar17h0123456789abcdefE.llvm.4242", "foo::bar"},
		{"_ZN3foo3barE", "foo::bar"},
		// from the Candle BERT guest
		{"_ZN4core3ptr74drop_in_place$LT$$LP$alloc..string..String$C$alloc..string..String$RP$$GT$17hb23fbf5cc4fcbd33E",
			"core::ptr::drop_in_place<(alloc::string::String,alloc::string::String)>"},
		{"_ZN73_$LT$P$u20$as$u20$clap_builder..builder..value_parser..AnyValueParser$GT$15possible_values17ha139dbc156e9fdf3E.llvm.3513887040217044828",
			"<P as clap_builder::builder::value_parser::AnyValueParser>::possible_values"},
	} {
		got, ok := demangleRust(tc.in)
		require.True(t, ok, tc.in)
		require.Equal(t, tc.want, got)
	}
	for _, in := range []string{
		"main", "memcpy", "_ZN3foo3barEv", // C++: parameter types follow the E
		"_ZN3fooE3bar", "_ZN9toolong", "_ZN", "_ZN3foo$XX$E",
	} {
		_, ok := demangleRust(in)
		require.False(t, ok, in)
	}
}

func TestShortName(t *testing.T) {
	for in, want := range map[string]string{
		"<candle_core::cpu_backend::MatMul as candle_core::cpu_backend::utils::Map2>::f": "<MatMul as Map2>::f",
		"core::ptr::drop_in_place<(alloc::string::String,alloc::string::String)>":        "core::ptr::drop_in_place<(String,String)>",
		"gemm_f32::microkernel::scalar::f32::x4x2x4":                                     "gemm_f32::microkernel::scalar::f32::x4x2x4",
		"alloc::vec::Vec<T,A>::reserve":                                                  "alloc::vec::Vec<T,A>::reserve",
		"matmul":                                                                         "matmul",
	} {
		require.Equal(t, want, ShortName(in), in)
	}
	require.Equal(t, "..f32::x4x2x4", Fit("gemm_f32::microkernel::scalar::f32::x4x2x4", 13))
	require.Equal(t, "softm..", Fit("softmax_rows", 7))
}
