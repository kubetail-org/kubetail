use std::io::{Cursor, Read};

use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};

use rgkl::util::reader::{LogTrimmerReader, ReverseLineReader};

fn log_trimmer_reader_bench(c: &mut Criterion) {
    let mut base_line = b"2024-11-20T10:00:00Z stdout F ".to_vec();
    base_line.extend_from_slice(&vec![b'a'; 4096]); // message payload
    base_line.push(b'\n');

    let mut data = Vec::new();
    for _ in 0..256 {
        data.extend_from_slice(&base_line);
    }

    let mut group = c.benchmark_group("log_trimmer_reader");
    group.throughput(Throughput::Bytes(data.len() as u64));

    for limit in [0u64, 64, 1024, 4096] {
        group.bench_with_input(BenchmarkId::from_parameter(limit), &limit, |b, &limit| {
            b.iter(|| {
                let cursor = Cursor::new(data.as_slice());
                let mut reader = LogTrimmerReader::new(cursor, limit);
                let mut sink = Vec::with_capacity(data.len());
                reader.read_to_end(&mut sink).unwrap();
            });
        });
    }

    group.finish();
}

fn reverse_line_reader_bench(c: &mut Criterion) {
    let mut data = Vec::new();
    for i in 0..2000 {
        let line = format!(
            "2024-11-{:02}T10:00:00Z stdout F line_{:04}\n",
            (i % 28) + 1,
            i
        );
        data.extend_from_slice(line.as_bytes());
    }

    let max_pos = data.len() as u64;

    let mut group = c.benchmark_group("reverse_line_reader");
    group.throughput(Throughput::Bytes(max_pos));

    group.bench_function("read_all", |b| {
        b.iter(|| {
            let cursor = Cursor::new(data.as_slice());
            let mut reader = ReverseLineReader::new(cursor, 0, max_pos).unwrap();
            let mut sink = Vec::with_capacity(data.len());
            reader.read_to_end(&mut sink).unwrap();
        });
    });

    group.finish();
}

criterion_group!(reader_benches, log_trimmer_reader_bench, reverse_line_reader_bench);
criterion_main!(reader_benches);
